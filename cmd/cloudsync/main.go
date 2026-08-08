// cloudsync — 把文本/文件推送到可公开访问的云端存储，并拿到稳定 URL。
//
// 子命令：
//
//	push <目标> <文件> [--all] [--name 远程名] [--verify true|false]
//	read <目标> <远程名>
//	delete <目标> <远程名>
//	list
//	config-check
//	version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloudsync/internal/cloudsync"
	_ "cloudsync/internal/provider/cfkv"
	_ "cloudsync/internal/provider/edgeone"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "push":
		return cmdPush(stdout, stderr, rest)
	case "read":
		return cmdRead(stdout, stderr, rest)
	case "delete":
		return cmdDelete(stdout, stderr, rest)
	case "list":
		return cmdList(stdout, stderr, rest)
	case "config-check":
		return cmdConfigCheck(stdout, stderr, rest)
	case "version", "--version":
		fmt.Fprintln(stdout, "cloudsync "+version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "cloudsync: unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

// app carries parsed global state for one command invocation.
type app struct {
	stdout   io.Writer
	stderr   io.Writer
	verbose  bool
	cfg      *cloudsync.Config
	targets  map[string]cloudsync.Target
	redactor *cloudsync.Redactor
}

func newApp(stdout, stderr io.Writer, configPath string, verbose bool) (*app, error) {
	path := configPath
	if path == "" {
		path = cloudsync.FindConfig()
	}
	if path == "" {
		return nil, errors.New("cloudsync: no config file found (pass -config or create cloudsync.yaml; see cloudsync.yaml.example)")
	}
	cfg, err := cloudsync.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	targets, err := cfg.Build()
	if err != nil {
		return nil, err
	}
	a := &app{
		stdout: stdout, stderr: stderr, verbose: verbose,
		cfg: cfg, targets: targets,
		redactor: cloudsync.RedactConfigSecrets(cfg),
	}
	if verbose {
		fmt.Fprintf(stderr, "cloudsync: config %s, %d target(s)\n", path, len(cfg.Targets))
	}
	return a, nil
}

func (a *app) errf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(a.stderr, a.redactor.Redact(msg))
}

func (a *app) out(url string) {
	fmt.Fprintln(a.stdout, url)
}

// globalFlags registers -config/-verbose on a command flagset.
func globalFlags(fs *flag.FlagSet) (*string, *bool) {
	config := fs.String("config", "", "config file (default: $CLOUDSYNC_CONFIG, ./cloudsync.yaml, ~/.cloudsync.yaml)")
	verbose := fs.Bool("verbose", false, "verbose logging to stderr")
	return config, verbose
}

// valueFlags are the flags that consume a separate value argument, so
// reorderFlags can keep their values attached when interspersing.
var valueFlags = map[string]bool{
	"config": true, "name": true, "verify": true, "retries": true, "timeout": true,
}

// reorderFlags moves flags (and their values) ahead of positional arguments,
// letting users write `push target file --name x` as well as flags-first
// syntax, which the flag package would otherwise reject.
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		flags = append(flags, a)
		if eq := strings.Index(name, "="); eq >= 0 {
			continue // --flag=value carries its own value
		}
		if valueFlags[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}
func cmdPush(stdout, stderr io.Writer, rest []string) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config, verbose := globalFlags(fs)
	all := fs.Bool("all", false, "push to all configured targets")
	name := fs.String("name", "", "remote filename (default: base name of the file)")
	verify := fs.String("verify", "", `override read-back verification: "true" or "false"`)
	retries := fs.Int("retries", 0, "upload retries (0 = default)")
	timeout := fs.Duration("timeout", 0, "overall timeout (e.g. 90s; 0 = no timeout)")
	if err := fs.Parse(reorderFlags(rest)); err != nil {
		return 2
	}

	var targetName, file string
	switch {
	case *all && fs.NArg() == 1:
		file = fs.Arg(0)
	case !*all && fs.NArg() == 2:
		targetName, file = fs.Arg(0), fs.Arg(1)
	default:
		if *all {
			fmt.Fprintln(stderr, "usage: cloudsync push --all <file>")
		} else {
			fmt.Fprintln(stderr, "usage: cloudsync push <target> <file>")
		}
		return 2
	}

	a, err := newApp(stdout, stderr, *config, *verbose)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	var verifyOverride *bool
	switch *verify {
	case "":
	case "true":
		v := true
		verifyOverride = &v
	case "false":
		v := false
		verifyOverride = &v
	default:
		fmt.Fprintf(stderr, "cloudsync: -verify must be \"true\" or \"false\", got %q\n", *verify)
		return 2
	}

	data, err := os.ReadFile(file)
	if err != nil {
		a.errf("cloudsync: read %s: %v", file, err)
		return 1
	}
	remoteName := *name
	if remoteName == "" {
		remoteName = filepath.Base(file)
	}

	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	opts := cloudsync.PushOptions{Verify: verifyOverride, MaxRetries: *retries}

	if *all {
		names := a.cfg.SortedNames()
		targets := make([]cloudsync.Target, 0, len(names))
		for _, n := range names {
			targets = append(targets, a.targets[n])
		}
		results := cloudsync.Broadcast(ctx, targets, remoteName, data)
		ok := 0
		for _, r := range results {
			if r.Err != nil {
				a.errf("[%s] %v", r.Target, r.Err)
				continue
			}
			ok++
			fmt.Fprintf(stdout, "%s\t%s\n", r.Target, r.URL)
		}
		if ok < len(results) {
			a.errf("cloudsync: %d of %d target(s) failed", len(results)-ok, len(results))
			return 1
		}
		return 0
	}

	target, ok := a.targets[targetName]
	if !ok {
		a.errf("cloudsync: unknown target %q (available: %s)", targetName, strings.Join(a.cfg.SortedNames(), ", "))
		return 1
	}
	url, err := cloudsync.PushWithOptions(ctx, target, remoteName, data, opts)
	if err != nil {
		a.errf("%v", err)
		return 1
	}
	a.out(url)
	return 0
}

func cmdRead(stdout, stderr io.Writer, rest []string) int {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config, verbose := globalFlags(fs)
	if err := fs.Parse(reorderFlags(rest)); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: cloudsync read <target> <remote-name>")
		return 2
	}
	a, err := newApp(stdout, stderr, *config, *verbose)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	target, ok := a.targets[fs.Arg(0)]
	if !ok {
		a.errf("cloudsync: unknown target %q (available: %s)", fs.Arg(0), strings.Join(a.cfg.SortedNames(), ", "))
		return 1
	}
	data, err := target.Read(context.Background(), fs.Arg(1))
	if err != nil {
		a.errf("%v", err)
		return 1
	}
	if _, err := stdout.Write(data); err != nil {
		a.errf("cloudsync: write stdout: %v", err)
		return 1
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
	return 0
}

func cmdDelete(stdout, stderr io.Writer, rest []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config, verbose := globalFlags(fs)
	if err := fs.Parse(reorderFlags(rest)); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: cloudsync delete <target> <remote-name>")
		return 2
	}
	a, err := newApp(stdout, stderr, *config, *verbose)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	target, ok := a.targets[fs.Arg(0)]
	if !ok {
		a.errf("cloudsync: unknown target %q (available: %s)", fs.Arg(0), strings.Join(a.cfg.SortedNames(), ", "))
		return 1
	}
	if err := target.Delete(context.Background(), fs.Arg(1)); err != nil {
		a.errf("%v", err)
		return 1
	}
	fmt.Fprintf(stdout, "deleted %s from %s\n", fs.Arg(1), fs.Arg(0))
	return 0
}

func cmdList(stdout, stderr io.Writer, rest []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config, verbose := globalFlags(fs)
	if err := fs.Parse(reorderFlags(rest)); err != nil {
		return 2
	}
	a, err := newApp(stdout, stderr, *config, *verbose)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "NAME\tTYPE\tBASE_URL\tVERIFY\tTOKEN")
	for _, name := range a.cfg.SortedNames() {
		tc := a.cfg.Targets[name]
		verify := true
		if tc.Verify != nil {
			verify = *tc.Verify
		}
		token := "no"
		if tc.Token != "" {
			token = "***"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%v\t%s\n", name, tc.Type, tc.BaseURL, verify, token)
	}
	return 0
}

func cmdConfigCheck(stdout, stderr io.Writer, rest []string) int {
	fs := flag.NewFlagSet("config-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config, verbose := globalFlags(fs)
	if err := fs.Parse(reorderFlags(rest)); err != nil {
		return 2
	}
	path := *config
	if path == "" {
		path = cloudsync.FindConfig()
	}
	if path == "" {
		fmt.Fprintln(stderr, "cloudsync: no config file found (pass -config or create cloudsync.yaml)")
		return 1
	}
	cfg, err := cloudsync.LoadConfig(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// Build also validates provider construction (e.g. token-less targets are fine).
	if _, err := cfg.Build(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *verbose {
		for _, name := range cfg.SortedNames() {
			fmt.Fprintf(stderr, "  %s: %s\n", name, cfg.Targets[name].Type)
		}
	}
	fmt.Fprintf(stdout, "config OK: %s (%d targets)\n", path, len(cfg.Targets))
	return 0
}

func usage(w io.Writer) {
	fmt.Fprint(w, `cloudsync — 云端文本/文件推送同步工具

用法:
  cloudsync <command> [flags]

命令:
  push <target> <file>   上传文件到指定目标，输出公开 URL
       --all             广播到所有配置的目标（此时只给文件路径）
       --name <name>     远程文件名（默认取本地文件名）
       --verify true|false  覆盖回读校验设置
       --retries <n>     上传重试次数（0 = 默认）
       --timeout <dur>   总超时，如 90s
  read <target> <key>    回读远程内容并输出到 stdout
  delete <target> <key>  删除远程对象
  list                   列出配置的目标
  config-check           校验配置文件并退出
  version                输出版本号
  help                   显示本帮助

全局 flags（每个子命令均可带）:
  -config <path>   配置文件（默认查找 $CLOUDSYNC_CONFIG、./cloudsync.yaml、~/.cloudsync.yaml）
  -verbose         详细日志输出到 stderr

配置文件示例见 cloudsync.yaml.example；密钥用 ${ENV_VAR} 注入，不要写进仓库。
`)
}
