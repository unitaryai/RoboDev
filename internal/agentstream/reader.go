package agentstream

import (
	"bufio"
	"context"
	"io"
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Reader reads NDJSON lines from an io.ReadCloser and emits parsed
// StreamEvents on a channel. It is tolerant of malformed lines, logging
// parse errors and continuing to read the stream.
type Reader struct {
	logger     *slog.Logger
	translator Translator
}

// ReaderOption configures optional Reader behaviour.
type ReaderOption func(*Reader)

// WithTranslator configures the Reader to convert each raw log line into
// stream events using t, instead of the default passthrough translator
// (Osmia's native NDJSON envelope format, parsed via ParseEvent).
func WithTranslator(t Translator) ReaderOption {
	return func(r *Reader) {
		r.translator = t
	}
}

// NewReader creates a Reader with the given logger. By default it uses the
// passthrough translator; pass WithTranslator to plug in an engine-specific
// stream format translator.
func NewReader(logger *slog.Logger, opts ...ReaderOption) *Reader {
	r := &Reader{
		logger:     logger,
		translator: NewPassthroughTranslator(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ReadStream reads NDJSON lines from stream until EOF or context
// cancellation. Each successfully parsed line is sent to eventCh.
// Malformed lines are logged and skipped. The stream is closed when
// reading completes or the context is cancelled.
func (r *Reader) ReadStream(ctx context.Context, stream io.ReadCloser, eventCh chan<- *StreamEvent) error {
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		events, err := r.translator.Translate(line)
		if err != nil {
			r.logger.Warn("failed to parse agent stream line",
				"error", err,
				"line", string(line),
			)
			continue
		}

		for _, ev := range events {
			select {
			case eventCh <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// ReadPodLogs connects to a running Kubernetes pod's log stream and reads
// NDJSON events from the specified container. It delegates to ReadStream
// for the actual parsing. The follow option is set so that the stream
// remains open until the container exits or the context is cancelled.
func (r *Reader) ReadPodLogs(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, podName, containerName string,
	eventCh chan<- *StreamEvent,
) error {
	opts := &corev1.PodLogOptions{
		Follow:    true,
		Container: containerName,
	}

	stream, err := client.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return err
	}

	return r.ReadStream(ctx, stream, eventCh)
}
