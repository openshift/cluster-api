<div align="center">
<a href="https://cobra.dev">
<img width="512" height="535" alt="cobra-logo" src="https://github.com/user-attachments/assets/c8bf9aad-b5ae-41d3-8899-d83baec10af8" />
</a>
</div>

Cobra is used in many Go projects such as [Kubernetes](http://kubernetes.io/),
[Hugo](https://gohugo.io), and [Github CLI](https://github.com/cli/cli) to
name a few. [This list](./projects_using_cobra.md) contains a more extensive list of projects using Cobra.

<a href="https://cobra.dev">Visit Cobra.dev for extensive documentation</a> 


Cobra is used in many Go projects such as [Kubernetes](https://kubernetes.io/),
[Hugo](https://gohugo.io), and [GitHub CLI](https://github.com/cli/cli) to
name a few. [This list](site/content/projects_using_cobra.md) contains a more extensive list of projects using Cobra.

[![](https://img.shields.io/github/actions/workflow/status/spf13/cobra/test.yml?branch=main&longCache=true&label=Test&logo=github%20actions&logoColor=fff)](https://github.com/spf13/cobra/actions?query=workflow%3ATest)
[![Go Reference](https://pkg.go.dev/badge/github.com/spf13/cobra.svg)](https://pkg.go.dev/github.com/spf13/cobra)
[![Go Report Card](https://goreportcard.com/badge/github.com/spf13/cobra)](https://goreportcard.com/report/github.com/spf13/cobra)
[![Slack](https://img.shields.io/badge/Slack-cobra-brightgreen)](https://gophers.slack.com/archives/CD3LP1199)
<hr>
<div align="center" markdown="1">
   <sup>Supported by:</sup>
   <br>
   <br>
   <a href="https://www.warp.dev/cobra">
      <img alt="Warp sponsorship" width="400" src="https://github.com/user-attachments/assets/ab8dd143-b0fd-4904-bdc5-dd7ecac94eae">
   </a>

### [Warp, the AI terminal for devs](https://www.warp.dev/cobra)
[Try Cobra in Warp today](https://www.warp.dev/cobra)<br>

</div>
<hr>

# Table of Contents

- [Overview](#overview)
- [Concepts](#concepts)
  * [Commands](#commands)
  * [Flags](#flags)
- [Installing](#installing)
- [Usage](#usage)
  * [Using the Cobra Generator](user_guide.md#using-the-cobra-generator)
  * [Using the Cobra Library](user_guide.md#using-the-cobra-library)
  * [Working with Flags](user_guide.md#working-with-flags)
  * [Positional and Custom Arguments](user_guide.md#positional-and-custom-arguments)
  * [Example](user_guide.md#example)
  * [Help Command](user_guide.md#help-command)
  * [Usage Message](user_guide.md#usage-message)
  * [PreRun and PostRun Hooks](user_guide.md#prerun-and-postrun-hooks)
  * [Suggestions when "unknown command" happens](user_guide.md#suggestions-when-unknown-command-happens)
  * [Generating documentation for your command](user_guide.md#generating-documentation-for-your-command)
  * [Generating shell completions](user_guide.md#generating-shell-completions)
- [Contributing](CONTRIBUTING.md)
- [License](#license)

# Overview

Cobra is a library providing a simple interface to create powerful modern CLI
interfaces similar to git & go tools.

Cobra is also an application that will generate your application scaffolding to rapidly
develop a Cobra-based application.

Cobra provides:
* Easy subcommand-based CLIs: `app server`, `app fetch`, etc.
* Fully POSIX-compliant flags (including short & long versions)
* Nested subcommands
* Global, local and cascading flags
* Easy generation of applications & commands with `cobra init appname` & `cobra add cmdname`
* Intelligent suggestions (`app srver`... did you mean `app server`?)
* Automatic help generation for commands and flags
* Automatic help flag recognition of `-h`, `--help`, etc.
* Automatically generated shell autocomplete for your application (bash, zsh, fish, powershell)
* Automatically generated man pages for your application
* Command aliases so you can change things without breaking them
* The flexibility to define your own help, usage, etc.
* Optional tight integration with [viper](http://github.com/spf13/viper) for 12-factor apps

# Concepts

Cobra is built on a structure of commands, arguments & flags.

**Commands** represent actions, **Args** are things and **Flags** are modifiers for those actions.

The best applications read like sentences when used, and as a result, users
intuitively know how to interact with them.

The pattern to follow is
`APPNAME VERB NOUN --ADJECTIVE.`
    or
`APPNAME COMMAND ARG --FLAG`

A few good real world examples may better illustrate this point.

In the following example, 'server' is a command, and 'port' is a flag:

    hugo server --port=1313

In this command we are telling Git to clone the url bare.

    git clone URL --bare

## Commands

Command is the central point of the application. Each interaction that
the application supports will be contained in a Command. A command can
have children commands and optionally run an action.

In the example above, 'server' is the command.

[More about cobra.Command](https://godoc.org/github.com/spf13/cobra#Command)

## Flags

A flag is a way to modify the behavior of a command. Cobra supports
fully POSIX-compliant flags as well as the Go [flag package](https://golang.org/pkg/flag/).
A Cobra command can define flags that persist through to children commands
and flags that are only available to that command.

In the example above, 'port' is the flag.

Flag functionality is provided by the [pflag
library](https://github.com/spf13/pflag), a fork of the flag standard library
which maintains the same interface while adding POSIX compliance.

# Installing
Using Cobra is easy. First, use `go get` to install the latest version
of the library. This command will install the `cobra` generator executable
along with the library and its dependencies:

    go get -u github.com/spf13/cobra

Next, include Cobra in your application:

```go
import "github.com/spf13/cobra"
```

# Usage

See [User Guide](user_guide.md).

# License

Cobra is released under the Apache 2.0 license. See [LICENSE.txt](https://github.com/spf13/cobra/blob/master/LICENSE.txt)
