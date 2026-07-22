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

const setFlagName = "set"

type prepareInit func(dotruntime.Overrides, string) (dotruntime.InitInputs, error)

type beginInit func(dotruntime.Overrides) (*dotruntime.InitSession, error)

type closeInit func(*dotruntime.InitSession) error

type initDecisions struct {
	selection dotruntime.InitSelection
	apply     bool
}

func newInitCommand(env environment, global *globalOptions) *cobra.Command {
	var setValues []string
	var yes bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize or update the machine configuration",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runInit(command, global, setValues, yes, env)
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&setValues, setFlagName, nil, "set a declared data value (key=value)")
	flags.BoolVarP(&yes, yesFlagName, "y", false, "apply immediately and confirm whole-module pruning")
	return command
}

func runInit(
	command *cobra.Command,
	global *globalOptions,
	setValues []string,
	yes bool,
	env environment,
) (resultErr error) {
	selectedData, err := parseInitSetValues(setValues)
	if err != nil {
		return err
	}
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
	inputs, err := prepare(overrides, env.build.Version)
	if err != nil {
		return err
	}
	decisions, err := resolveInitDecisions(inputs, selectedData, yes, env.openInitTTY)
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
	loaded, err := session.Load(env.build.Version)
	if err != nil {
		return err
	}
	publication, err := loaded.CommitConfig(candidate)
	if err != nil {
		return err
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
		if inputs.Compatibility().DevelopmentBuild() {
			if _, err := fmt.Fprintln(command.ErrOrStderr(), "notice: development build skipped the requires version comparison"); err != nil {
				return fmt.Errorf("write init compatibility notice: %w", err)
			}
		}
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
		Runtime:    overrides,
		CLIVersion: env.build.Version,
		Confirm:    confirmationCallback(command, yes, env.openTerminal),
		Stdin:      command.InOrStdin(),
		Stdout:     command.OutOrStdout(),
		Stderr:     command.ErrOrStderr(),
	}, child)
	return finishMutationApply(command, result, runErr, global.verbose)
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

// parseInitSetValues 保留每次 --set 的 presence，并保守拒绝同一 key 的重复赋值。
func parseInitSetValues(values []string) (map[string]dotruntime.Override, error) {
	selections := make(map[string]dotruntime.Override, len(values))
	for _, value := range values {
		key, selected, ok := strings.Cut(value, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --set %q: want key=value", value)
		}
		if _, duplicate := selections[key]; duplicate {
			return nil, fmt.Errorf("duplicate --set key %q", key)
		}
		selections[key] = dotruntime.Override{Value: selected, Set: true}
	}
	return selections, nil
}

// resolveInitDecisions 在调用方仍未取 lock 的阶段闭合 profile、data 与 apply 决策。
// --yes 使用无歧义的旧值/default，只有缺少必要值时才要求用户终端。
func resolveInitDecisions(
	inputs dotruntime.InitInputs,
	setValues map[string]dotruntime.Override,
	yes bool,
	openTerminal func() (io.ReadWriteCloser, error),
) (decisions initDecisions, resultErr error) {
	declarations := inputs.Manifest().DataDeclarations()
	declared := make(map[string]struct{}, len(declarations))
	for _, declaration := range declarations {
		declared[declaration.Key()] = struct{}{}
	}
	for key := range setValues {
		if _, ok := declared[key]; !ok {
			return initDecisions{}, fmt.Errorf("unknown init data key %q", key)
		}
	}

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

	existing, hasExisting := context.ExistingMachine()
	missingData := make(map[string]struct{})
	for _, declaration := range declarations {
		key := declaration.Key()
		if _, ok := setValues[key]; ok {
			continue
		}
		if hasExisting {
			if _, ok := existing.Data()[key]; ok {
				continue
			}
		}
		if _, ok := declaration.Default(); !ok {
			missingData[key] = struct{}{}
		}
	}

	needsTerminal := !yes || !profileResolved || len(missingData) > 0
	selection := dotruntime.InitSelection{Data: cloneInitSelections(setValues)}
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

	for _, declaration := range declarations {
		key := declaration.Key()
		if _, explicit := selection.Data[key]; explicit {
			continue
		}
		if yes {
			if _, required := missingData[key]; !required {
				continue
			}
		}
		prompt, ok := declaration.Prompt()
		if !ok {
			prompt = key
		}
		defaultValue, hasDefault := initDataDefault(existing, hasExisting, key)
		if !hasDefault {
			defaultValue, hasDefault = declaration.Default()
		}
		selected, err := promptInitData(reader, terminal, prompt, defaultValue, hasDefault)
		if err != nil {
			return initDecisions{}, err
		}
		selection.Data[key] = dotruntime.Override{Value: selected, Set: true}
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

func promptInitData(
	reader *bufio.Reader,
	writer io.Writer,
	prompt string,
	defaultValue string,
	hasDefault bool,
) (string, error) {
	label := prompt + ": "
	if hasDefault {
		label = fmt.Sprintf("%s [%s]: ", prompt, defaultValue)
	}
	answer, err := readInitAnswer(reader, writer, label)
	if err != nil {
		return "", err
	}
	if answer == "" && hasDefault {
		return defaultValue, nil
	}
	return answer, nil
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

func initDataDefault(existing dotruntime.MachineContext, hasExisting bool, key string) (string, bool) {
	if !hasExisting {
		return "", false
	}
	value, ok := existing.Data()[key]
	return value, ok
}

func cloneInitSelections(values map[string]dotruntime.Override) map[string]dotruntime.Override {
	cloned := make(map[string]dotruntime.Override, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
