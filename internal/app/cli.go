package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"sauron-sees/internal/capture"
	"sauron-sees/internal/codex"
	"sauron-sees/internal/config"
	"sauron-sees/internal/dailysummary"
	"sauron-sees/internal/platform"
	"sauron-sees/internal/scheduler"
	"sauron-sees/internal/startup"
	"sauron-sees/internal/state"
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
		return rt.runAgent(ctx)
	case "close-day":
		fs := flag.NewFlagSet("close-day", flag.ContinueOnError)
		fs.SetOutput(c.stderr)
		date := fs.String("date", "", "date to close in YYYY-MM-DD")
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
		return rt.closeDay(ctx, day)
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
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		if *week != "" {
			return rt.closeWeek(ctx, *week)
		}
		if *from != "" || *to != "" {
			if *from == "" || *to == "" {
				return fmt.Errorf("weekly-summary requires both --from and --to")
			}
			return rt.closeWeekRange(ctx, *from, *to)
		}
		return rt.closeWeek(ctx, rt.planner.WeekKey(time.Now()))
	case "doctor":
		rt, err := c.bootstrap()
		if err != nil {
			return err
		}
		defer rt.close()
		results := dailysummary.Doctor(rt.cfg, rt.runner)
		for _, result := range results {
			status := "OK"
			if !result.OK {
				status = "FAIL"
			}
			fmt.Fprintf(c.stdout, "[%s] %s: %s\n", status, result.Name, result.Message)
		}
		if dailysummary.HasBlockingIssue(results) {
			return fmt.Errorf("doctor found blocking issues")
		}
		return nil
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
	fileState, err := state.Load(layout.StatePath())
	if err != nil {
		return nil, err
	}
	logger, err := newLogger(layout.LogPath(), c.stdout)
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
		state:      fileState,
		logger:     logger,
		host:       host,
		runner:     runner,
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
	}, nil
}

type runtimeEnv struct {
	cfg        config.Config
	configPath string
	planner    *scheduler.Planner
	layout     workspace.Layout
	state      *state.FileState
	logger     *logger
	host       platform.Host
	runner     codex.Runner
	capturer   capture.Service
	summaries  dailysummary.Service
	weekly     weeklysummary.Service
}

func (r *runtimeEnv) saveState() error {
	return r.state.Save(r.layout.StatePath())
}

func (r *runtimeEnv) close() error {
	return r.logger.Close()
}

func (r *runtimeEnv) currentDay(now time.Time) string {
	return r.planner.LocalDate(now)
}

func newLogger(path string, stdout io.Writer) (*logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &logger{file: file, stdout: stdout}, nil
}
