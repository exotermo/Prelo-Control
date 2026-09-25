package queue

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	streamKey  = "hermes:execution-jobs"
	groupName  = "hermes-workers"
	fieldJobID = "job_id"
)

// Stream is a thin wrapper over a single Redis Stream + consumer group, used purely as a
// low-latency dispatch/wake signal (see ADR-013) — it is never the source of truth for
// whether a job exists or needs (re)processing. If Redis is unreachable, flushed, or a
// message is lost before any consumer reads it, no execution job is lost: the sweeper
// (etapa G11) discovers claimable/orphaned jobs from Postgres alone, independent of this
// stream.
type Stream struct {
	client *redis.Client
}

func NewStream(client *redis.Client) *Stream {
	return &Stream{client: client}
}

// EnsureGroup creates the consumer group (and the stream, if missing) once at startup.
// BUSYGROUP (group already exists) is expected and ignored.
func (s *Stream) EnsureGroup(ctx context.Context) error {
	err := s.client.XGroupCreateMkStream(ctx, streamKey, groupName, "$").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}
	return nil
}

// Publish is a best-effort wake-up signal, sent after the enqueuing transaction has already
// committed the durable Postgres rows. A failure here is logged and swallowed — it costs
// dispatch latency, never correctness (the sweeper's poll loop is the fallback).
func (s *Stream) Publish(ctx context.Context, jobID string) {
	if err := s.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{fieldJobID: jobID},
	}).Err(); err != nil {
		log.Printf("queue: best-effort publish failed for job %s: %v", jobID, err)
	}
}

// Consume blocks, reading new messages for consumerName in groupName, and invokes handler
// with each message's job id. It ACKs immediately after handler returns — regardless of
// outcome — because Redis's role here is "notify", not "hold work"; whether the job actually
// needs retrying is decided from Postgres state (by the handler itself, or later by the
// sweeper), never by Redis redelivery/PEL semantics. Returns when ctx is done.
func (s *Stream) Consume(ctx context.Context, consumerName string, handler func(ctx context.Context, jobID string) error) {
	for {
		if ctx.Err() != nil {
			return
		}
		result, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    groupName,
			Consumer: consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			log.Printf("queue: consume error, retrying: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range result {
			for _, msg := range stream.Messages {
				jobID, _ := msg.Values[fieldJobID].(string)
				if jobID != "" {
					if err := handler(ctx, jobID); err != nil {
						log.Printf("queue: handler error for job %s (sweeper will reconcile): %v", jobID, err)
					}
				}
				s.client.XAck(ctx, streamKey, groupName, msg.ID)
			}
		}
	}
}

func isBusyGroup(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP")
}
