package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var timerCalibrationMode = flag.Bool("timer-calibration", false, "calibrate the asynchronous delay timer used by Experiment 3")

func runTimerCalibration() error {
	rows := [][]string{{"requested_delay_ms", "samples", "avg_timer_ms", "timer_variance", "min_timer_ms", "max_timer_ms"}}
	for _, text := range strings.Split(*latencyLevels, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil || d < 0 {
			return fmt.Errorf("invalid latency %q", text)
		}
		values := []float64{}
		minimum, maximum := 1e30, 0.0
		for i := 0; i < 25; i++ {
			started := time.Now()
			done := make(chan time.Duration, 1)
			time.AfterFunc(d, func() { done <- time.Since(started) })
			value := durationMS(<-done)
			values = append(values, value)
			if value < minimum {
				minimum = value
			}
			if value > maximum {
				maximum = value
			}
		}
		mean, variance := meanVariance(values)
		rows = append(rows, []string{formatFloat(durationMS(d)), "25", formatFloat(mean), formatFloat(variance), formatFloat(minimum), formatFloat(maximum)})
	}
	path := filepath.Join(*requestTimingOutput, "timer_calibration.csv")
	if err := ensureParent(path); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.WriteAll(rows); err != nil {
		return err
	}
	return appendTimerCalibration(*requestTimingOutput)
}

func appendTimerCalibration(dir string) error {
	file, err := os.Open(filepath.Join(dir, "timer_calibration.csv"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := csv.NewReader(file).ReadAll()
	file.Close()
	if err != nil {
		return err
	}
	reportPath := filepath.Join(dir, "report.md")
	report, err := os.ReadFile(reportPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	text := strings.Split(string(report), "\n## Timer calibration")[0]
	text += "\n## Timer calibration\n\nThe same time.AfterFunc primitive was measured independently without networking, using 25 serial samples per delay. This diagnoses host timer/scheduler overhead; it does not replace the ideal 2L reference with a fitted curve. Serial calibration is a diagnostic, not a precise prediction of parallel lookup timing.\n\n| requested delay ms | measured timer mean ms | variance | min ms | max ms |\n|---:|---:|---:|---:|---:|\n"
	for _, row := range rows[1:] {
		text += fmt.Sprintf("| %s | %s | %s | %s | %s |\n", row[0], row[2], row[3], row[4], row[5])
	}
	text += "\nCompare standalone calibration with the networked run before attributing deviations to timer resolution. If standalone delays track the requested values while networked RTTs plateau, serial timer overhead does not reproduce that gap; concurrent scheduling, queueing and host load need separate profiling. This diagnostic runs separately and does not establish a single cause. Close to the RPC deadline, scheduling overhead also creates late or missing observations even without injected packet loss. The CSV retains response fractions and late-response counts so the received-only mean cannot conceal those events.\n"
	return os.WriteFile(reportPath, []byte(text), 0644)
}
