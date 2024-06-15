package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/chzyer/readline"
)

var (
	history         []string
	aliases         map[string]string
	envVars         map[string]string
	builtins        map[string]bool
	jobs            []*exec.Cmd
	mu              sync.Mutex
	commandCache    map[string]string
	cacheExpiration time.Duration = 5 * time.Minute
	completer 		*readline.PrefixCompleter

	// Customization variables
	shellBgOpacity   int
	shellTextSize    int
	shellTextColor   string
	shellTextBold    bool
	shellPromptStyle string

	app      *tview.Application
	textView *tview.TextView
	input    string
)

func getAllCommands() []string {
    commands := make([]string, 0, len(builtins))

    for cmd := range builtins {
        commands = append(commands, cmd)
    }

    pathEnv := os.Getenv("PATH")
    paths := strings.Split(pathEnv, string(os.PathListSeparator))
    for _, path := range paths {
        files, err := os.ReadDir(path)
        if err != nil {
            continue
        }
        for _, file := range files {
            commands = append(commands, file.Name())
        }
    }

    return commands
}

func init() {
	aliases = make(map[string]string)
	envVars = make(map[string]string)
	commandCache = make(map[string]string)
	builtins = map[string]bool{
		"echo":    true,
		"exit":    true,
		"type":    true,
		"pwd":     true,
		"cd":      true,
		"whoami":  true,
		"ls":      true,
		"cat":     true,
		"touch":   true,
		"rm":      true,
		"mkdir":   true,
		"rmdir":   true,
		"history": true,
		"clear":   true,
		"alias":   true,
		"unalias": true,
		"export":  true,
		"unset":   true,
		"jobs":    true,
		"fg":      true,
		"bg":      true,
		"kill":    true,
		"shell":   true, // Added shell customization command
	}

	// Default customization settings
	shellBgOpacity = 100
	shellTextSize = 12
	shellTextColor = "white"
	shellTextBold = false
	shellPromptStyle = "default"

	completer = readline.NewPrefixCompleter()
    for _, cmd := range getAllCommands() {
        completer.Children = append(completer.Children, readline.PcItem(cmd))
    }
	
	go startCPUProfile()
}

func startCPUProfile() {
	f, err := os.Create("cpu.prof")
	if err != nil {
		fmt.Println("could not create CPU profile: ", err)
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Println("could not start CPU profile: ", err)
		return
	}
	time.Sleep(30 * time.Second)
	pprof.StopCPUProfile()
}

func main() {
	defer pprof.StopCPUProfile()

	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("Error getting current user: %v\n", err)
		os.Exit(1)
	}

	homeDir := currentUser.HomeDir

	// Load aliases and environment variables from file
	loadAliasesAndEnvVars(filepath.Join(homeDir, ".my_shell_aliases"))
	loadEnvVars(filepath.Join(homeDir, ".my_shell_env"))

	// Initialize tcell screen
	app = tview.NewApplication()
	textView = tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWordWrap(true).
		SetChangedFunc(func() {
			app.Draw()
		})

	textView.SetBorder(true).SetTitle("Dyshell")

	// Capture key events for input
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			cmdLine := strings.TrimSpace(input)
			input = ""
			fmt.Fprint(textView, "\n") // Add newline before handling command
			handleCommand(cmdLine)
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if len(input) > 0 {
				input = input[:len(input)-1]
			}
		case tcell.KeyRune:
			input += string(event.Rune())
		case tcell.KeyUp:
			if len(history) > 0 {
				if input == "" {
					input = history[len(history)-1]
				} else {
					for i := len(history) - 1; i >= 0; i-- {
						if history[i] == input && i > 0 {
							input = history[i-1]
							break
						}
					}
				}
			}
		case tcell.KeyDown:
			if len(history) > 0 {
				for i := 0; i < len(history); i++ {
					if history[i] == input && i < len(history)-1 {
						input = history[i+1]
						break
					}
				}
			}
		case tcell.KeyTab:
			suggestions, _ := completer.Do([]rune(input), len(input))
			if len(suggestions) > 0 {
				input = string(suggestions[0])
			}
		}
		updatePrompt()
		return nil
	})

	// Initial prompt
	updatePrompt()

	if err := app.SetRoot(textView, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}
}

func updatePrompt() {
	currentDir, err := os.Getwd()
	if err != nil {
		currentDir = "~"
	}
	textView.Clear()
	fmt.Fprintf(textView, "%s %s%s_", currentDir, getPrompt(), input) // Added cursor indicator "_"
	app.Draw() // Explicitly draw the application
}

func getPrompt() string {
	return "> "
}

func handleCommand(cmdLine string) {
    fmt.Fprintf(textView, "Executing command: %s\n", cmdLine) // Debugging statement
    cmdLine = strings.TrimSpace(cmdLine)
    if cmdLine == "" {
        updatePrompt()
        return
    }

    // Save command to history
    mu.Lock()
    history = append(history, cmdLine)
    mu.Unlock()

    // Perform command substitution
    cmdLine = substituteCommand(cmdLine)

    // Expand environment variables
    cmdLine = os.ExpandEnv(cmdLine)

    // Check for multiline command
    if strings.HasSuffix(cmdLine, "\\") {
        input += "\n"
        updatePrompt()
        return
    }

    // Check for piped commands
    if strings.Contains(cmdLine, "|") {
        executePipedCommands(cmdLine)
        updatePrompt()
        return
    }

    // Capture output
    output := new(strings.Builder)
    writer := io.MultiWriter(output, textView)

    // Execute built-in command
    args := strings.Split(cmdLine, " ")
    cmd := args[0]

    // Check for aliases
    if aliasCmd, ok := aliases[cmd]; ok {
        cmd = aliasCmd
        args = append([]string{cmd}, args[1:]...)
    }

    if builtins[cmd] {
        executeBuiltinCommand(cmd, args[1:], writer)
    } else {
        // Check for background job
        if strings.HasSuffix(cmdLine, "&") {
            cmdLine = strings.TrimSuffix(cmdLine, "&")
            args = strings.Fields(cmdLine)
            cmd := exec.Command(args[0], args[1:]...)
            cmd.Stdout = writer
            cmd.Stderr = writer
            err := cmd.Start()
            if err == nil {
                mu.Lock()
                jobs = append(jobs, cmd)
                mu.Unlock()
                fmt.Fprintf(writer, "[%d] %d\n", len(jobs), cmd.Process.Pid)
            } else {
                fmt.Fprintf(writer, "%s: %v\n", cmd.Args[0], err)
            }
        } else {
            // Check for redirection
            if strings.Contains(cmdLine, ">") || strings.Contains(cmdLine, "<") {
                executeRedirectedCommand(cmdLine, writer)
            } else {
                // Search for the command in PATH and execute it
                if path, found := getCachedCommandPath(cmd); found {
                    executeExternalCommand(path, args[1:], writer)
                } else {
                    pathEnv := os.Getenv("PATH")
                    paths := strings.Split(pathEnv, string(os.PathListSeparator))
                    found := false
                    for _, path := range paths {
                        fullPath := filepath.Join(path, cmd)
                        if _, err := os.Stat(fullPath); err == nil {
                            cacheCommandPath(cmd, fullPath)
                            found = true
                            executeExternalCommand(fullPath, args[1:], writer)
                            break
                        }
                    }
                    if !found {
                        fmt.Fprintf(writer, "%s: command not found\n", cmd)
                    }
                }
            }
        }
    }

    // Display prompt again
    updatePrompt()
}



// executeBuiltinCommand executes built-in shell commands.
func executeBuiltinCommand(cmd string, args []string, writer io.Writer) {
	switch cmd {
	case "echo":
		fmt.Fprintln(writer, strings.Join(args, " "))
	case "exit":
		saveAliasesAndEnvVars(filepath.Join(userHomeDir(), ".my_shell_aliases"))
		saveEnvVars(filepath.Join(userHomeDir(), ".my_shell_env"))
		os.Exit(0)
	case "type":
		if len(args) > 0 {
			arg := args[0]
			if builtins[arg] {
				fmt.Fprintf(writer, "%s is a shell builtin\n", arg)
			} else {
				path, found := getCachedCommandPath(arg)
				if found {
					fmt.Fprintf(writer, "%s is %s\n", arg, path)
				} else {
					pathEnv := os.Getenv("PATH")
					paths := strings.Split(pathEnv, string(os.PathListSeparator))
					found := false
					for _, path := range paths {
						fullPath := filepath.Join(path, arg)
						if _, err := os.Stat(fullPath); err == nil {
							fmt.Fprintf(writer, "%s is %s\n", arg, fullPath)
							cacheCommandPath(arg, fullPath)
							found = true
							break
						}
					}
					if !found {
						fmt.Fprintf(writer, "%s not found\n", arg)
					}
				}
			}
		}
	case "pwd":
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(writer, "Error: %v\n", err)
		} else {
			fmt.Fprintln(writer, dir)
		}
	case "cd":
		if len(args) > 0 {
			dir := args[0]
			if dir == "~" {
				dir = userHomeDir()
			}
			err := os.Chdir(dir)
			if err != nil {
				fmt.Fprintf(writer, "cd: %s: No such file or directory\n", dir)
			}
			updatePrompt() // Add this line to update the prompt after changing directory
		} else {
			os.Chdir(userHomeDir())
			updatePrompt() // Add this line to update the prompt after changing directory
		}
	case "whoami":
		user, err := user.Current()
		if err != nil {
			fmt.Fprintf(writer, "Error: %v\n", err)
		} else {
			fmt.Fprintln(writer, user.Username)
		}
	case "ls":
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		files, err := os.ReadDir(path)
		if err != nil {
			fmt.Fprintf(writer, "ls: cannot access '%s': %v\n", path, err)
			return
		}
		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				continue
			}
			modTime := info.ModTime().Format("Jan 02 15:04")
			size := info.Size()
			fmt.Fprintf(writer, "%-20s %10d %s\n", file.Name(), size, modTime)
		}	
	case "cat":
		if len(args) > 0 {
			for _, file := range args {
				data, err := os.ReadFile(file)
				if err != nil {
					fmt.Fprintf(writer, "cat: cannot read '%s': %v\n", file, err)
					continue
				}
				fmt.Fprint(writer, string(data))
			}
		} else {
			fmt.Fprintln(writer, "cat: missing file operand")
		}
	case "touch":
		if len(args) > 0 {
			for _, file := range args {
				f, err := os.Create(file)
				if err != nil {
					fmt.Fprintf(writer, "touch: cannot create '%s': %v\n", file, err)
					continue
				}
				f.Close()
			}
		} else {
			fmt.Fprintln(writer, "touch: missing file operand")
		}
	case "rm":
		if len(args) > 0 {
			for _, file := range args {
				err := os.Remove(file)
				if err != nil {
					fmt.Fprintf(writer, "rm: cannot remove '%s': %v\n", file, err)
					continue
				}
			}
		} else {
			fmt.Fprintln(writer, "rm: missing file operand")
		}
	case "mkdir":
		if len(args) > 0 {
			for _, dir := range args {
				err := os.Mkdir(dir, 0755)
				if err != nil {
					fmt.Fprintf(writer, "mkdir: cannot create directory '%s': %v\n", dir, err)
					continue
				}
			}
		} else {
			fmt.Fprintln(writer, "mkdir: missing directory operand")
		}
	case "rmdir":
		if len(args) > 0 {
			for _, dir := range args {
				err := os.Remove(dir)
				if err != nil {
					fmt.Fprintf(writer, "rmdir: cannot remove directory '%s': %v\n", dir, err)
					continue
				}
			}
		} else {
			fmt.Fprintln(writer, "rmdir: missing directory operand")
		}
	case "history":
		mu.Lock()
		for i, cmd := range history {
			fmt.Fprintf(writer, "%d %s\n", i+1, cmd)
		}
		mu.Unlock()
	case "clear":
		cmd := exec.Command("clear")
		cmd.Stdout = writer
		cmd.Run()
	case "alias":
		if len(args) == 0 {
			mu.Lock()
			for k, v := range aliases {
				fmt.Fprintf(writer, "alias %s='%s'\n", k, v)
			}
			mu.Unlock()
		} else {
			for _, alias := range args {
				parts := strings.SplitN(alias, "=", 2)
				if len(parts) == 2 {
					mu.Lock()
					aliases[parts[0]] = strings.Trim(parts[1], "'\"")
					mu.Unlock()
				}
			}
		}
	case "help":
		fmt.Fprintln(writer, "Available commands:")
		for cmd := range builtins {
			fmt.Fprintf(writer, "  %s\n", cmd)
		}
		fmt.Fprintln(writer, "Use `man <command>` for more information on a command.")
	case "unalias":
		if len(args) > 0 {
			for _, alias := range args {
				mu.Lock()
				delete(aliases, alias)
				mu.Unlock()
			}
		}
	case "export":
		if len(args) > 0 {
			for _, envVar := range args {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) == 2 {
					os.Setenv(parts[0], parts[1])
					mu.Lock()
					envVars[parts[0]] = parts[1]
					mu.Unlock()
				}
			}
		}
	case "unset":
		if len(args) > 0 {
			for _, envVar := range args {
				os.Unsetenv(envVar)
				mu.Lock()
				delete(envVars, envVar)
				mu.Unlock()
			}
		}
	case "jobs":
		mu.Lock()
		for i, job := range jobs {
			fmt.Fprintf(writer, "[%d]+  %d Running    %s\n", i+1, job.Process.Pid, strings.Join(job.Args, " "))
		}
		mu.Unlock()
	case "fg":
		if len(args) > 0 {
			jobNumber, err := strconv.Atoi(args[0])
			if err == nil && jobNumber > 0 && jobNumber <= len(jobs) {
				mu.Lock()
				job := jobs[jobNumber-1]
				mu.Unlock()
				job.Wait()
			} else {
				fmt.Fprintf(writer, "fg: %s: no such job\n", args[0])
			}
		}
	case "bg":
		if len(args) > 0 {
			jobNumber, err := strconv.Atoi(args[0])
			if err == nil && jobNumber > 0 && jobNumber <= len(jobs) {
				mu.Lock()
				job := jobs[jobNumber-1]
				mu.Unlock()
				var _ *exec.Cmd = job
				err := sendSignalContinue()
				if err != nil {
					fmt.Fprintf(writer, "Failed to send continue signal: %v\n", err)
				}
			} else {
				fmt.Fprintf(writer, "bg: %s: no such job\n", args[0])
			}
		}
	case "kill":
		if len(args) > 0 {
			pid, err := strconv.Atoi(args[0])
			if err == nil {
				process, err := os.FindProcess(pid)
				if err == nil {
					err = process.Kill()
					if err == nil {
						fmt.Fprintf(writer, "Process %d killed\n", pid)
					} else {
						fmt.Fprintf(writer, "Failed to kill process %d: %v\n", pid, err)
					}
				} else {
					fmt.Fprintf(writer, "Failed to find process %d: %v\n", pid, err)
				}
			} else {
				fmt.Fprintf(writer, "Invalid PID: %s\n", args[0])
			}
		} else {
			fmt.Fprintln(writer, "kill: missing PID operand")
		}
	case "shell":
		if len(args) > 0 {
			handleShellCustomization(args, writer)
		} else {
			printShellCustomization(writer)
		}
	}
}

func handleShellCustomization(args []string, writer io.Writer) {
	if len(args) < 2 {
		fmt.Fprintln(writer, "Usage: shell [option] [value]")
		return
	}

	option := args[0]
	value := strings.Join(args[1:], " ")

	switch option {
	case "bg-opacity":
		opacity, err := strconv.Atoi(value)
		if err == nil && opacity >= 0 && opacity <= 100 {
			shellBgOpacity = opacity
			fmt.Fprintf(writer, "Background opacity set to %d%%\n", shellBgOpacity)
		} else {
			fmt.Fprintln(writer, "Invalid opacity value. Please enter a value between 0 and 100.")
		}
	case "text-size":
		size, err := strconv.Atoi(value)
		if err == nil && size > 0 {
			shellTextSize = size
			fmt.Fprintf(writer, "Text size set to %d\n", shellTextSize)
		} else {
			fmt.Fprintln(writer, "Invalid text size. Please enter a positive integer.")
		}
	case "text-color":
		shellTextColor = value
		fmt.Fprintf(writer, "Text color set to %s\n", shellTextColor)
	case "text-bold":
		if value == "true" {
			shellTextBold = true
			fmt.Fprintln(writer, "Text bold set to true")
		} else if value == "false" {
			shellTextBold = false
			fmt.Fprintln(writer, "Text bold set to false")
		} else {
			fmt.Fprintln(writer, "Invalid value for text-bold. Use true or false.")
		}
	case "prompt-style":
		shellPromptStyle = value
		fmt.Fprintf(writer, "Prompt style set to %s\n", shellPromptStyle)
	default:
		fmt.Fprintln(writer, "Unknown customization option.")
	}
}

func printShellCustomization(writer io.Writer) {
	fmt.Fprintln(writer, "Shell Customization Options:")
	fmt.Fprintf(writer, "bg-opacity: %d%%\n", shellBgOpacity)
	fmt.Fprintf(writer, "text-size: %d\n", shellTextSize)
	fmt.Fprintf(writer, "text-color: %s\n", shellTextColor)
	fmt.Fprintf(writer, "text-bold: %t\n", shellTextBold)
	fmt.Fprintf(writer, "prompt-style: %s\n", shellPromptStyle)
}

func executeExternalCommand(path string, args []string, writer io.Writer) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			fmt.Fprintf(writer, "%s: %v\n", cmd.Args[0], exitError)
		} else if os.IsPermission(err) {
			fmt.Fprintf(writer, "%s: permission denied\n", cmd.Args[0])
		} else if os.IsNotExist(err) {
			fmt.Fprintf(writer, "%s: command not found\n", cmd.Args[0])
		} else {
			fmt.Fprintf(writer, "%s: %v\n", cmd.Args[0], err)
		}
	}
}

// Cache command path with expiration
func cacheCommandPath(cmd, path string) {
	mu.Lock()
	commandCache[cmd] = path
	mu.Unlock()
	time.AfterFunc(cacheExpiration, func() {
		mu.Lock()
		delete(commandCache, cmd)
		mu.Unlock()
	})
}

func getCachedCommandPath(cmd string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	path, found := commandCache[cmd]
	return path, found
}

// loadEnvVars loads environment variables from a file.
func loadEnvVars(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			mu.Lock()
			envVars[parts[0]] = parts[1]
			mu.Unlock()
			os.Setenv(parts[0], parts[1])
		}
	}
}

// saveEnvVars saves environment variables to a file.
func saveEnvVars(filepath string) {
	file, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Error saving environment variables: %v\n", err)
		return
	}
	defer file.Close()

	mu.Lock()
	for k, v := range envVars {
		fmt.Fprintf(file, "%s=%s\n", k, v)
	}
	mu.Unlock()
}

// substituteCommand performs command substitution.
func substituteCommand(cmdLine string) string {
	for {
		start := strings.Index(cmdLine, "$(")
		if start == -1 {
			break
		}
		end := strings.Index(cmdLine[start:], ")")
		if end == -1 {
			break
		}
		end += start
		subCmd := cmdLine[start+2 : end]
		output, err := exec.Command("/bin/sh", "-c", subCmd).Output()
		if err == nil {
			cmdLine = cmdLine[:start] + strings.TrimSpace(string(output)) + cmdLine[end+1:]
		}
	}
	return cmdLine
}

// AutoCompleter implements readline's AutoCompleter interface.
type AutoCompleter struct{}

// Do completes the given prefix and returns the suggestions.
func (a *AutoCompleter) Do(line []rune, pos int) ([][]rune, int) {
	var suggestions [][]rune
	prefix := string(line[:pos])
	for cmd := range builtins {
		if strings.HasPrefix(cmd, prefix) {
			suggestions = append(suggestions, []rune(cmd))
		}
	}
	pathEnv := os.Getenv("PATH")
	paths := strings.Split(pathEnv, string(os.PathListSeparator))
	for _, path := range paths {
		files, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, file := range files {
			if strings.HasPrefix(file.Name(), prefix) {
				suggestions = append(suggestions, []rune(file.Name()))
			}
		}
	}
	return suggestions, len(prefix)
}

// executePipedCommands executes piped commands.
func executePipedCommands(cmdLine string) {
	commands := strings.Split(cmdLine, "|")
	var cmds []*exec.Cmd

	for _, cmdStr := range commands {
		cmdArgs := strings.Fields(strings.TrimSpace(cmdStr))
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		cmds = append(cmds, cmd)
	}

	var lastStdout io.ReadCloser
	for i, cmd := range cmds {
		if i != 0 {
			cmd.Stdin = lastStdout
		}
		if i != len(cmds)-1 {
			stdout, _ := cmd.StdoutPipe()
			lastStdout = stdout
		} else {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
		cmd.Start()
	}
	for _, cmd := range cmds {
		cmd.Wait()
	}
}

func executeRedirectedCommand(cmdLine string, writer io.Writer) {
	var cmdArgs []string
	var redirectOut, redirectIn string
	redirectOutIndex := strings.Index(cmdLine, ">")
	redirectInIndex := strings.Index(cmdLine, "<")
	if redirectOutIndex != -1 {
		cmdArgs = strings.Fields(strings.TrimSpace(cmdLine[:redirectOutIndex]))
		redirectOut = strings.TrimSpace(cmdLine[redirectOutIndex+1:])
	} else if redirectInIndex != -1 {
		cmdArgs = strings.Fields(strings.TrimSpace(cmdLine[:redirectInIndex]))
		redirectIn = strings.TrimSpace(cmdLine[redirectInIndex+1:])
	} else {
		cmdArgs = strings.Fields(cmdLine)
	}

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	if redirectOut != "" {
		outFile, err := os.Create(redirectOut)
		if err != nil {
			fmt.Fprintf(writer, "Error creating file %s: %v\n", redirectOut, err)
			return
		}
		defer outFile.Close()
		cmd.Stdout = outFile
	}
	if redirectIn != "" {
		inFile, err := os.Open(redirectIn)
		if err != nil {
			fmt.Fprintf(writer, "Error opening file %s: %v\n", redirectIn, err)
			return
		}
		defer inFile.Close()
		cmd.Stdin = inFile
	}
	cmd.Stderr = writer
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(writer, "%s: %v\n", cmdArgs[0], err)
	}
}

// loadAliasesAndEnvVars loads aliases and environment variables from a file.
func loadAliasesAndEnvVars(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "alias ") {
			parts := strings.SplitN(line[6:], "=", 2)
			if len(parts) == 2 {
				aliases[parts[0]] = strings.Trim(parts[1], "'\"")
			}
		} else {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				mu.Lock()
				envVars[parts[0]] = parts[1]
				mu.Unlock()
				os.Setenv(parts[0], parts[1])
			}
		}
	}
}

// saveAliasesAndEnvVars saves aliases and environment variables to a file.
func saveAliasesAndEnvVars(filepath string) {
	file, err := os.Create(filepath)
	if err != nil {
		fmt.Printf("Error saving aliases and environment variables: %v\n", err)
		return
	}
	defer file.Close()

	mu.Lock()
	for k, v := range aliases {
		fmt.Fprintf(file, "alias %s='%s'\n", k, v)
	}
	for k, v := range envVars {
		fmt.Fprintf(file, "%s=%s\n", k, v)
	}
	mu.Unlock()
}

// sendSignalContinue is a placeholder for the SIGCONT signal handling in Windows.
func sendSignalContinue() error {
	return errors.New("SIGCONT not supported on Windows")
}

// userHomeDir gets the user's home directory.
func userHomeDir() string {
	user, err := user.Current()
	if err != nil {
		return ""
	}
	return user.HomeDir
}
