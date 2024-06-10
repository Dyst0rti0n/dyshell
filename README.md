# Dyshell: A Customizable Interactive Shell

Dyshell is a highly customizable interactive shell built using Go. It offers a range of built-in commands, supports job control, command history, aliases, and environment variable management. This shell aims to provide an enhanced user experience with features like command substitution, redirection, piping, and auto-completion. Dyshell is designed to be easily extensible, allowing users to add their own custom commands and quick commands to streamline their workflow.

## README for Discord

---

### Dyshell: A Customizable Interactive Shell

Welcome to Dyshell, a feature-rich and customizable interactive shell built with Go. Whether you're a developer looking for a flexible shell environment or just someone who loves to customize their command line experience, Dyshell has something for you.

---

### Features

- **Built-in Commands**: Essential commands like `echo`, `exit`, `pwd`, `cd`, `ls`, `cat`, `touch`, `rm`, `mkdir`, `rmdir`, `history`, `clear`, `alias`, `unalias`, `export`, `unset`, `jobs`, `fg`, `bg`, `kill`.
- **Job Control**: Manage background and foreground jobs.
- **Command Substitution**: Support for command substitution using `$()`.
- **Redirection and Piping**: Easily redirect input and output and pipe commands together.
- **Auto-Completion**: Intelligent auto-completion for commands and file paths.
- **Custom Aliases and Environment Variables**: Define and manage your own aliases and environment variables.
- **Persistent History**: Command history is saved across sessions.
- **Quick Commands**: Define quick commands to speed up your workflow.
- **Customizable Prompt**: Set your own shell prompt to personalize your environment.

---

### Getting Started

#### Prerequisites

- Go 1.16 or later installed on your system.

#### Installation

Clone the repository:

```sh
git clone https://github.com/yourusername/dyshell.git
cd dyshell
```

Build the project:

```sh
go build -o dyshell main.go
```

Run Dyshell:

```sh
./dyshell
```

---

### Usage

Once you have Dyshell up and running, you can start using it just like any other shell. Here are some examples to get you started:

```sh
# Navigate to a directory
cd /path/to/directory

# List files in the current directory
ls

# Create a new file
touch myfile.txt

# Display the contents of a file
cat myfile.txt

# Set an alias
alias ll='ls -la'

# Use the alias
ll

# Create a directory
mkdir mydir

# Remove a file
rm myfile.txt

# Remove a directory
rmdir mydir

# Show command history
history

# Clear the screen
clear

# Set an environment variable
export MYVAR=myvalue

# Use command substitution
echo $(ls)
```

---

### Customization

#### Aliases

Define your own aliases to save time. For example:

```sh
alias gs='git status'
```

#### Environment Variables

Set environment variables that persist across sessions:

```sh
export MYVAR=myvalue
```

#### Quick Commands

Add quick commands to streamline repetitive tasks.

#### Custom Prompt

Personalize your shell prompt by editing the `prompt` variable in the configuration file.

---

### Contributing

We welcome contributions! Feel free to submit issues and pull requests to improve Dyshell.

---

### License

This project is licensed under the MIT License.
