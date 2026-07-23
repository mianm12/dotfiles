package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	applyrunner "github.com/mianm12/dotfiles/internal/apply"
	dotruntime "github.com/mianm12/dotfiles/internal/runtime"
	"github.com/spf13/cobra"
)

type prepareInit func(dotruntime.Overrides) (dotruntime.InitInputs, error)

type beginInit func(dotruntime.Overrides) (*dotruntime.InitSession, error)

type closeInit func(*dotruntime.InitSession) error

type initDecisions struct {
	selection dotruntime.InitSelection
	apply     bool
}

func newInitCommand(env environment, global *globalOptions) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize or update the machine configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runInit(command, global, yes, env)
		},
	}
	flags := command.Flags()
	flags.BoolVarP(&yes, yesFlagName, "y", false, "apply immediately and confirm whole-module pruning")
	return command
}

func runInit(
	command *cobra.Command,
	global *globalOptions,
	yes bool,
	env environment,
) (resultErr error) {
	overrides := dotruntime.Overrides{
		Home: dotruntime.Override{
			Value: global.home,
			Set:   command.Flags().Changed(homeFlagName),
		},
		Repository: dotruntime.Override{
			Value: global.repo,
			Set:   command.Flags().Changed(repoFlagName),
		},
		Profile: dotruntime.Override{
			Value: global.profile,
			Set:   command.Flags().Changed(profileFlagName),
		},
	}
	prepare := env.prepareInit
	if prepare == nil {
		prepare = dotruntime.PrepareInit
	}
	inputs, err := prepare(overrides)
	if err != nil {
		return err
	}
	decisions, err := resolveInitDecisions(inputs, yes, env.openInitTTY)
	if err != nil {
		return err
	}
	candidate, err := inputs.BuildCandidate(decisions.selection)
	if err != nil {
		return err
	}

	begin := env.beginInit
	if begin == nil {
		begin = dotruntime.BeginInit
	}
	session, err := begin(overrides)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("begin init returned nil session")
	}
	closeSession := env.closeInit
	if closeSession == nil {
		closeSession = func(session *dotruntime.InitSession) error { return session.Close() }
	}
	defer func() {
		resultErr = finishInitClose(resultErr, closeSession(session))
	}()
	loaded, err := session.Load()
	if err != nil {
		return err
	}
	publication, err := loaded.CommitConfig(candidate)
	if err != nil {
		return err
	}
	lockedInputs := loaded.Inputs()
	contextLine := fmt.Sprintf(
		"repo=%s profile=%s os=%s",
		lockedInputs.Context().Control().RepositoryPath(),
		candidate.Machine().Profile,
		env.goos,
	)
	if _, err := fmt.Fprintln(command.OutOrStdout(), contextLine); err != nil {
		return fmt.Errorf("write init context: %w", err)
	}
	status := "unchanged"
	if publication.Changed() {
		status = "updated"
	}
	if _, err := fmt.Fprintf(
		command.OutOrStdout(),
		"config  %s  (%s)\n",
		inputs.Context().Control().ConfigPath(),
		status,
	); err != nil {
		return fmt.Errorf("write init config result: %w", err)
	}
	if !decisions.apply {
		return nil
	}

	child, err := session.BeginMutation(overrides)
	if err != nil {
		return err
	}
	runner := env.applyNested
	if runner == nil {
		runner = applyrunner.RunWithMutationSession
	}
	result, runErr := runner(applyrunner.Options{
		Runtime: overrides,
		Confirm: confirmationCallback(command, yes, env.openTerminal),
		Stdin:   command.InOrStdin(),
		Stdout:  command.OutOrStdout(),
		Stderr:  command.ErrOrStderr(),
	}, child)
	return finishMutationApply(command, result, runErr, global.verbose, false)
}

// finishInitClose 让 unlock/close 失败高于纯 action/conflict 退出码，同时保留普通错误的聚合语义。
func finishInitClose(resultErr, closeErr error) error {
	if closeErr == nil {
		return resultErr
	}
	// 这里只提升 package 内直接返回的 sealed command exit；wrapped error 必须走 Join 保留全部 cause。
	requested, ok := resultErr.(commandExitError) //nolint:errorlint // 精确类型判定是本 helper 的契约边界。
	if ok {
		return fmt.Errorf("close init session after command exit %d: %w", requested.code, closeErr)
	}
	return errors.Join(resultErr, closeErr)
}

// resolveInitDecisions 在调用方仍未取 lock 的阶段闭合 profile 与 apply 决策。
func resolveInitDecisions(
	inputs dotruntime.InitInputs,
	yes bool,
	openTerminal func() (io.ReadWriteCloser, error),
) (decisions initDecisions, resultErr error) {
	context := inputs.Context()
	profiles := inputs.Manifest().ProfileNames()
	profileResolved := false
	if override, ok := context.ProfileOverride(); ok {
		if !slices.Contains(profiles, override) {
			return initDecisions{}, fmt.Errorf("unknown init profile %q", override)
		}
		profileResolved = true
	} else if existing, ok := context.ExistingMachine(); ok && slices.Contains(profiles, existing.Profile()) {
		profileResolved = true
	}

	needsTerminal := !yes || !profileResolved
	selection := dotruntime.InitSelection{}
	if !needsTerminal {
		candidate, err := inputs.BuildCandidate(selection)
		if err != nil {
			return initDecisions{}, err
		}
		_ = candidate
		return initDecisions{selection: selection, apply: true}, nil
	}

	if openTerminal == nil {
		openTerminal = func() (io.ReadWriteCloser, error) {
			return os.OpenFile("/dev/tty", os.O_RDWR, 0)
		}
	}
	terminal, err := openTerminal()
	if err != nil {
		return initDecisions{}, fmt.Errorf("open user terminal for init: %w", err)
	}
	if terminal == nil {
		return initDecisions{}, errors.New("open user terminal for init: returned nil terminal")
	}
	defer func() {
		resultErr = errors.Join(resultErr, terminal.Close())
	}()
	reader := bufio.NewReader(terminal)

	if !profileResolved {
		selected, err := promptInitProfile(reader, terminal, profiles)
		if err != nil {
			return initDecisions{}, err
		}
		selection.Profile = dotruntime.Override{Value: selected, Set: true}
	}

	applyNow := true
	if !yes {
		applyNow, err = promptInitApply(reader, terminal)
		if err != nil {
			return initDecisions{}, err
		}
	}
	if _, err := inputs.BuildCandidate(selection); err != nil {
		return initDecisions{}, err
	}
	return initDecisions{selection: selection, apply: applyNow}, nil
}

func promptInitProfile(reader *bufio.Reader, writer io.Writer, profiles []string) (string, error) {
	if _, err := fmt.Fprintln(writer, "Profiles:"); err != nil {
		return "", err
	}
	for _, profile := range profiles {
		if _, err := fmt.Fprintln(writer, "  "+profile); err != nil {
			return "", err
		}
	}
	for {
		answer, err := readInitAnswer(reader, writer, "Profile: ")
		if err != nil {
			return "", err
		}
		if slices.Contains(profiles, answer) {
			return answer, nil
		}
		if _, err := fmt.Fprintf(writer, "Unknown profile %q; choose one of the listed profiles.\n", answer); err != nil {
			return "", err
		}
	}
}

func promptInitApply(reader *bufio.Reader, writer io.Writer) (bool, error) {
	for {
		answer, err := readInitAnswer(reader, writer, "Apply now? [Y/n] ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if _, err := fmt.Fprintln(writer, "Answer yes or no."); err != nil {
				return false, err
			}
		}
	}
}

func readInitAnswer(reader *bufio.Reader, writer io.Writer, prompt string) (string, error) {
	if _, err := io.WriteString(writer, prompt); err != nil {
		return "", err
	}
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read init answer: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(answer, "\n"), "\r"), nil
}
