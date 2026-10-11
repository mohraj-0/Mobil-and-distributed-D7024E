package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type checkpointAttempt struct {
	From, To, RPC                                          string
	RequestDropped, ReplyAttempted, ReplyDropped, Received bool
	Started                                                time.Time
	RTT                                                    time.Duration
	OnTime                                                 bool
}

type checkpointResult struct {
	N, Alpha, K, Trial, Replicas, Probes, Rounds int
	Seed                                         int64
	Loss, Expected, ElapsedMS                    float64
	Kind, Source, Target                         string
	Success, ReturnedContact                     bool
	Attempts                                     []checkpointAttempt
	Latency                                      time.Duration
}

func writeLossCheckpoint(path string, results []lossResult) error {
	var records []checkpointResult
	for _, r := range results {
		record := checkpointResult{N: r.n, Alpha: r.alpha, K: r.k, Trial: r.trial, Replicas: r.replicas, Probes: r.probes, Rounds: r.rounds, Seed: r.seed, Loss: r.loss, Expected: r.expected, ElapsedMS: r.elapsedMS, Kind: r.kind, Source: r.source, Target: r.target, Success: r.success, ReturnedContact: r.returnedContact}
		record.Latency = r.latency
		for _, a := range r.attempts {
			record.Attempts = append(record.Attempts, checkpointAttempt{From: a.from, To: a.to, RPC: a.rpc, RequestDropped: a.requestDropped, ReplyAttempted: a.replyAttempted, ReplyDropped: a.replyDropped, Received: a.received, Started: a.started.UTC(), RTT: a.rtt, OnTime: a.onTime})
		}
		records = append(records, record)
	}
	payload, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", payload, 0644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func readLossCheckpoint(path string) ([]lossResult, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []checkpointResult
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, err
	}
	var results []lossResult
	for _, r := range records {
		result := lossResult{n: r.N, alpha: r.Alpha, k: r.K, trial: r.Trial, replicas: r.Replicas, probes: r.Probes, rounds: r.Rounds, seed: r.Seed, loss: r.Loss, expected: r.Expected, elapsedMS: r.ElapsedMS, kind: r.Kind, source: r.Source, target: r.Target, success: r.Success, returnedContact: r.ReturnedContact}
		result.latency = r.Latency
		for _, a := range r.Attempts {
			result.attempts = append(result.attempts, lossAttempt{from: a.From, to: a.To, rpc: a.RPC, requestDropped: a.RequestDropped, replyAttempted: a.ReplyAttempted, replyDropped: a.ReplyDropped, received: a.Received, started: a.Started, rtt: a.RTT, onTime: a.OnTime})
		}
		results = append(results, result)
	}
	return results, nil
}
