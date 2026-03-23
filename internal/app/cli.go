package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"sauron-sees/internal/agent"
	"sauron-sees/internal/capture"
	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
	"sauron-sees/internal/dailysummary"
	"sauron-sees/internal/platform"
	"sauron-sees/internal/process"
	"sauron-sees/internal/scheduler"
	"sauron-sees/internal/startup"
	"sauron-sees/internal/state"
	"sauron-sees/internal/tray"
	"sauron-sees/internal/weeklysummary"
	"sauron-sees/internal/workspace"
)

const taskName = "SauronSeesAgent"

type CLI struct {
	configPath string
	stdout     io.Writer
	stderr     io.Writer
}

func NewCLI(configPath string, stdout io.Writer, stderr io.Writer) CLI {
	return CLI{
		configPath: configPath,
		stdout:     stdout,
		stderr:     stderr,
	}
}

func (c CLI) Run(ctx context.Context, args []string) error {
	switch args[0] {
	case "agent":
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		if err := rt.acquireAgentLease(); err != nil {
			return err
		}
		return rt.runAgent(ctx)
	case "close-day":
		fs := flag.NewFlagSet("close-day", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		date := fs.String("date", "", "date to close in YYYY-MM-DD")
		dryRun := fs.Bool("dry-run", false, "render outputs without mutating state or deleting inputs")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		day := *date
		if day == "" {
			day = rt.planner.LocalDate(time.Now())
		}
		return rt.closeDay(ctx, day, *dryRun)
	case "capture-now":
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		return rt.captureNow(time.Now())
	case "weekly-summary":
		fs := flag.NewFlagSet("weekly-summary", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		week := fs.String("week", "", "ISO week to summarize, e.g. 2026-W10")
		from := fs.String("from", "", "start date YYYY-MM-DD")
		to := fs.String("to", "", "end date YYYY-MM-DD")
		dryRun := fs.Bool("dry-run", false, "render outputs without mutating state")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		if *week != "" {
			return rt.closeWeek(ctx, *week, *dryRun)
		}
		if *from != "" || *to != "" {
			if *from == "" || *to == "" {
				return fmt.Errorf("weekly-summary requires both --from and --to")
			}
			return rt.closeWeekRange(ctx, *from, *to, *dryRun)
		}
		return rt.closeWeek(ctx, rt.planner.WeekKey(time.Now()), *dryRun)
	case "doctor":
		fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		results := dailysummary.Doctor(rt.cfg, rt.runner)
		if *jsonOut {
			data, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(c.stdout, string(data))
		} else {
			for _, result := range results {
				status := "OK"
				if !result.OK {
					status = "FAIL"
				}
				fmt.Fprintf(c.stdout, "[%s] %s: %s\n", status, result.Name, result.Message)
			}
		}
		if dailysummary.HasBlockingIssue(results) {
			return fmt.Errorf("doctor found blocking issues")
		}
		return nil
	case "status":
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		status, err := rt.statusString(time.Now())
		if err != nil {
			return err
		}
		fmt.Fprint(c.stdout, status)
		return nil
	case "pause":
		fs := flag.NewFlagSet("pause", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		duration := fs.Duration("duration", time.Hour, "pause duration")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		return rt.pause(*duration)
	case "resume":
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		return rt.resume()
	case "stop":
		manager, err := c.agentManager()
		if err != nil {
			return err
		}
		return stopAgent(manager)
	case "install-startup":
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable: %w", err)
		}
		path, err := config.ResolvePath(c.configPath)
		if err != nil {
			return err
		}
		return startup.Install(taskName, executable, path)
	case "uninstall-startup":
		return startup.Uninstall(taskName)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c CLI) bootstrap() (*runtimeEnv, error) {
	cfg, configPath, err := config.Load(c.configPath)
	if err != nil {
		return nil, err
	}
	planner, err := scheduler.New(cfg)
	if err != nil {
		return nil, err
	}
	layout := workspace.Layout{
		TempRoot:           cfg.TempRoot,
		DailyMarkdownRoot:  cfg.DailyMarkdownRoot,
		WeeklyMarkdownRoot: cfg.WeeklyMarkdownRoot,
	}
	stateStore := state.NewStore(layout.StatePath())
	fileState, err := stateStore.Load()
	if err != nil {
		return nil, err
	}
	logger, err := newLogger(layout.LogPath(), c.stdout, cfg.Logging)
	if err != nil {
		return nil, err
	}
	host := platform.NewRealHost()
	runner := codex.ExecRunner{}
	return &runtimeEnv{
		cfg:        cfg,
		configPath: configPath,
		planner:    planner,
		layout:     layout,
		stateStore: stateStore,
		state:      fileState,
		logger:     logger,
		host:       host,
		runner:     runner,
		agentMgr:   agent.NewManager(layout.PIDPath(), layout.StopPath()),
		capturer: capture.Service{
			Host:        host,
			Layout:      layout,
			ImageMaxDim: cfg.ImageMaxDimension,
			JPEGQuality: cfg.JPEGQuality,
		},
		summaries: dailysummary.Service{
			Config: cfg,
			Layout: layout,
			Runner: runner,
		},
		weekly: weeklysummary.Service{
			Config: cfg,
			Layout: layout,
			Runner: runner,
		},
		trayEnabled: cfg.TrayEnabled,
	}, nil
}

func (c CLI) agentManager() (agent.Manager, error) {
	cfg, _, err := config.Load(c.configPath)
	if err != nil {
		return agent.Manager{}, err
	}
	layout := workspace.Layout{
		TempRoot:           cfg.TempRoot,
		DailyMarkdownRoot:  cfg.DailyMarkdownRoot,
		WeeklyMarkdownRoot: cfg.WeeklyMarkdownRoot,
	}
	return agent.NewManager(layout.PIDPath(), layout.StopPath()), nil
}

type runtimeEnv struct {
	cfg         config.Config
	configPath  string
	planner     *scheduler.Planner
	layout      workspace.Layout
	stateStore  state.Store
	state       *state.FileState
	logger      *logger
	host        platform.Host
	runner      codex.Runner
	agentMgr    agent.Manager
	agentLease  *agent.Lease
	capturer    capture.Service
	summaries   dailysummary.Service
	weekly      weeklysummary.Service
	trayEnabled bool
}

func (r *runtimeEnv) acquireAgentLease() error {
	lease, err := r.agentMgr.Acquire()
	if err != nil {
		var runningErr agent.AlreadyRunningError
		if errors.As(err, &runningErr) {
			return fmt.Errorf("agent already running with pid %d", runningErr.PID)
		}
		return err
	}
	r.agentLease = lease
	return nil
}

func (r *runtimeEnv) close() error {
	var result error
	if r.agentLease != nil {
		if err := r.agentLease.Close(); err != nil && result == nil {
			result = err
		}
		r.agentLease = nil
	}
	if r.logger != nil {
		if err := r.logger.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func (r *runtimeEnv) currentDay(now time.Time) string {
	return r.planner.LocalDate(now)
}

func (r *runtimeEnv) formatTime(ts time.Time) string {
	return ts.In(r.planner.Location()).Format(time.RFC3339)
}

func (r *runtimeEnv) reloadState() error {
	st, err := r.stateStore.Load()
	if err != nil {
		return err
	}
	r.state = st
	return nil
}

func (r *runtimeEnv) updateState(fn func(*state.FileState) error) error {
	st, err := r.stateStore.Update(fn)
	if err != nil {
		return err
	}
	r.state = st
	return nil
}

func newLogger(path string, stdout io.Writer, loggingCfg config.LoggingConfig) (*logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := openLogSink(path, loggingCfg)
	if err != nil {
		return nil, err
	}
	return &logger{file: file, stdout: stdout}, nil
}

func (r *runtimeEnv) trayOptions(ctx context.Context, stop func()) tray.Options {
	return tray.Options{
		Tooltip:         "Sauron Sees",
		OnCaptureNow:    func() { _ = r.captureNow(time.Now()) },
		OnCloseDay:      func() { _ = r.closeDay(ctx, r.currentDay(time.Now()), false) },
		OnWeeklySummary: func() { _ = r.closeWeek(ctx, r.planner.WeekKey(time.Now()), false) },
		OnPause:         func() { _ = r.pause(time.Hour) },
		OnResume:        func() { _ = r.resume() },
		OnOpenDaily:     func() { _ = openFolder(r.layout.DailyMarkdownRoot) },
		OnOpenWeekly:    func() { _ = openFolder(r.layout.WeeklyMarkdownRoot) },
		OnOpenTemp:      func() { _ = openFolder(r.layout.TempRoot) },
		OnDoctor: func() {
			results := dailysummary.Doctor(r.cfg, r.runner)
			for _, result := range results {
				r.logger.Printf("doctor %s: %s", result.Name, result.Message)
			}
		},
		OnExit: stop,
	}
}

func stopAgent(manager agent.Manager) error {
	pid, err := manager.ReadPID()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("agent is not running")
		}
		return fmt.Errorf("resolve running agent: %w", err)
	}
	if !process.IsRunning(pid) {
		_ = os.Remove(manager.PIDPath())
		_ = manager.ClearStopRequest()
		return fmt.Errorf("agent is not running (stale pid file for pid %d removed)", pid)
	}
	if err := manager.RequestStop(pid); err != nil {
		return err
	}
	signalErr := process.SignalStop(pid)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !process.IsRunning(pid) {
			_ = manager.ClearStopRequest()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if signalErr != nil {
		return fmt.Errorf("requested graceful stop for pid %d, but interrupt failed: %w", pid, signalErr)
	}
	return fmt.Errorf("timed out waiting for pid %d to stop", pid)
}
