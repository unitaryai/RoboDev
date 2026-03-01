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
	logger *slog.Logger
}

// NewReader creates a Reader with the given logger.
func NewReader(logger *slog.Logger) *Reader {
	return &Reader{logger: logger}
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

		ev, err := ParseEvent(line)
		if err != nil {
			r.logger.Warn("failed to parse agent stream line",
				"error", err,
				"line", string(line),
			)
			continue
		}

		select {
		case eventCh <- ev:
		case <-ctx.Done():
			return ctx.Err()
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
