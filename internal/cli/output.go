package cli

import (
	"fmt"
	"io"
)

// errorTrackingWriter 记录首次写入错误，使不返回错误的 Cobra 输出方法仍能影响最终退出码。
// 首次失败后，后续写入直接返回同一错误，不再调用底层 writer。
type errorTrackingWriter struct {
	writer io.Writer
	err    error
}

func (w *errorTrackingWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}

	written, err := w.writer.Write(data)
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return written, err
}

// commandOutput 分别跟踪 stdout 和 stderr 的首次写入错误。
type commandOutput struct {
	stdout errorTrackingWriter
	stderr errorTrackingWriter
}

func newCommandOutput(stdout, stderr io.Writer) *commandOutput {
	return &commandOutput{
		stdout: errorTrackingWriter{writer: stdout},
		stderr: errorTrackingWriter{writer: stderr},
	}
}

// finish 把输出写入失败提升为命令错误；stdout 失败时尽可能经 stderr 报告。
func (o *commandOutput) finish(code int) int {
	if o.stdout.err != nil {
		_, _ = fmt.Fprintf(&o.stderr, "error: write stdout: %v\n", o.stdout.err)
		return exitError
	}
	if o.stderr.err != nil {
		return exitError
	}
	return code
}
