// SPDX-License-Identifier: MPL-2.0

// Package console exposes local vault operations without starting a terminal UI.
package console

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"lefalock/internal/passwordgen"
)

// Commands have explicit flag allowlists: a misspelled or irrelevant option
// must fail before opening the vault or reading a secret.
var commands = map[string]string{
	"vault list": "query", "vault init": "", "vault rotate-key": "yes",
	"folder list": "query", "folder create": "name parent", "folder rename": "id name", "folder move": "id parent", "folder delete": "id yes",
	"service list": "query folder", "service create": "name secret-name folder type input generate generator", "service rename": "id name", "service move": "id folder", "service delete": "id yes",
	"secret list": "query service folder", "secret create": "name service folder type input generate generator", "secret set": "id input generate generator", "secret rename": "id name", "secret move": "id folder", "secret delete": "id yes", "secret show": "id reveal output", "secret code": "id reveal output", "secret copy": "id ttl",
	"note list": "query service folder", "note create": "name service folder input", "note set": "id input", "note rename": "id name", "note move": "id service folder", "note delete": "id yes", "note show": "id reveal output", "note copy": "id ttl",
	"password generate": "generator reveal output",
	"backup create":     "directory phrase-output", "backup preview": "file phrase decisions", "backup restore": "file phrase decisions yes",
	"recovery status": "", "recovery begin": "yes", "recovery empty": "yes", "recovery continue-empty": "yes", "recovery preview": "file phrase", "recovery restore": "file phrase yes", "recovery activate": "yes", "recovery cancel": "yes", "recovery inspect-pointer": "", "recovery repair-pointer": "yes",
}

type Request struct {
	Command                                                    string
	ID, Name, SecretName, Parent, Folder, Service, Query, Type string
	File, Directory, Decisions, PhraseOutput, Output           string
	Yes, Reveal, Generate                                      bool
	TTL                                                        time.Duration
	Generator                                                  passwordgen.Options
	Input                                                      SecretInput
	set                                                        map[string]bool
	Help                                                       bool
}

func Handles(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "vault", "folder", "service", "secret", "note", "password", "backup", "recovery":
		return true
	}
	return false
}

func Help(w io.Writer) {
	fmt.Fprintln(w, "Vault commands (no TUI; metadata is JSON):")
	keys := make([]string, 0, len(commands))
	for k := range commands {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintln(w, "  lefalock "+k+" [options]")
	}
	fmt.Fprintln(w, "\nUse <group> <command> --help for options. IDs come from list commands.")
	fmt.Fprintln(w, "Secrets: hidden terminal prompt, --value-stdin, --value-file, or --value-fd.")
	fmt.Fprintln(w, "Recovery phrases use --phrase-stdin / --phrase-file / --phrase-fd.")
	fmt.Fprintln(w, "Secret output requires --reveal (raw stdout) or --output (new private file).")
	fmt.Fprintln(w, "Delete, restore, recovery changes and rotation require --yes.")
}

func Parse(args []string, output io.Writer) (Request, error) {
	r := Request{Generator: passwordgen.DefaultOptions(), TTL: 30 * time.Second, Type: "password", Input: SecretInput{FD: -1}}
	if len(args) < 2 || (len(args) == 2 && (args[1] == "--help" || args[1] == "help" || args[1] == "-h")) {
		Help(output)
		r.Help = true
		return r, nil
	}
	r.Command = args[0] + " " + args[1]
	spec, ok := commands[r.Command]
	if !ok {
		return r, errors.New("unknown vault command; run lefalock vault --help")
	}
	fs := flag.NewFlagSet(r.Command, flag.ContinueOnError)
	fs.SetOutput(io.Discard) // flag's errors may include a supplied secret: never echo arguments.
	for _, name := range strings.Fields(spec) {
		switch name {
		case "id":
			fs.StringVar(&r.ID, name, "", "Object ID")
		case "name":
			fs.StringVar(&r.Name, name, "", "Object name")
		case "secret-name":
			fs.StringVar(&r.SecretName, name, "", "First secret name")
		case "parent":
			fs.StringVar(&r.Parent, name, "", "Parent folder ID; empty means root")
		case "folder":
			fs.StringVar(&r.Folder, name, "", "Folder ID; empty means root")
		case "service":
			fs.StringVar(&r.Service, name, "", "Service ID; empty detaches a note")
		case "query":
			fs.StringVar(&r.Query, name, "", "Case-insensitive name search")
		case "type":
			fs.StringVar(&r.Type, name, "password", "password or auth_code")
		case "file":
			fs.StringVar(&r.File, name, "", "Encrypted backup file")
		case "directory":
			fs.StringVar(&r.Directory, name, "", "Backup directory; default is vault backups")
		case "decisions":
			fs.StringVar(&r.Decisions, name, "", "JSON file mapping conflict keys to restore decisions")
		case "phrase-output":
			fs.StringVar(&r.PhraseOutput, name, "", "Required new private file for recovery phrase")
		case "output":
			fs.StringVar(&r.Output, name, "", "Write raw secret to a new private file")
		case "yes":
			fs.BoolVar(&r.Yes, name, false, "Confirm this operation")
		case "reveal":
			fs.BoolVar(&r.Reveal, name, false, "Explicitly write the raw secret to stdout")
		case "ttl":
			fs.DurationVar(&r.TTL, name, 30*time.Second, "Wait before conditionally clearing clipboard (1s–5m)")
		case "generate":
			fs.BoolVar(&r.Generate, name, false, "Generate a password without displaying it")
		case "generator":
			fs.IntVar(&r.Generator.Length, "length", 20, "Password length (4–128)")
			fs.BoolVar(&r.Generator.Lower, "lower", true, "Include lowercase letters")
			fs.BoolVar(&r.Generator.Upper, "upper", true, "Include uppercase letters")
			fs.BoolVar(&r.Generator.Digits, "digits", true, "Include digits")
			fs.BoolVar(&r.Generator.Special, "special", true, "Include special characters")
		case "input", "phrase":
			prefix := "value"
			if name == "phrase" {
				prefix = "phrase"
			}
			fs.BoolVar(&r.Input.Stdin, prefix+"-stdin", false, "Read from stdin; remove one final LF/CRLF except for notes")
			fs.StringVar(&r.Input.File, prefix+"-file", "", "Read a private regular file (permissions 0600)")
			fs.IntVar(&r.Input.FD, prefix+"-fd", -1, "Read an inherited file descriptor")
		}
	}
	help := fs.Bool("help", false, "Show command help")
	fs.BoolVar(help, "h", false, "Show command help")
	if err := fs.Parse(args[2:]); err != nil {
		return r, errors.New("invalid command options; use --help (secret values must never be arguments)")
	}
	if fs.NArg() != 0 {
		return r, errors.New("positional values are not accepted; use named options and secure secret input")
	}
	if *help {
		fmt.Fprintln(output, "Usage: lefalock "+r.Command+" [options]")
		fs.SetOutput(output)
		fs.PrintDefaults()
		r.Help = true
		return r, nil
	}
	r.set = map[string]bool{}
	fs.Visit(func(f *flag.Flag) { r.set[f.Name] = true })
	return r, r.validate()
}

func (r Request) validate() error {
	if r.Input.sources() > 1 || (r.Generate && r.Input.sources() > 0) {
		return errors.New("select exactly one secret input source")
	}
	if r.set["value-fd"] || r.set["phrase-fd"] {
		if r.Input.FD < 0 {
			return errors.New("file descriptor must be nonnegative")
		}
	}
	if (r.set["value-file"] || r.set["phrase-file"]) && r.Input.File == "" {
		return errors.New("secret input file is required")
	}
	if r.Reveal && r.Output != "" {
		return errors.New("select --reveal or --output")
	}
	for _, key := range strings.Fields(commands[r.Command]) {
		switch key {
		case "id":
			if r.ID == "" {
				return errors.New("--id is required")
			}
		case "name":
			if strings.TrimSpace(r.Name) == "" {
				return errors.New("--name is required")
			}
		case "secret-name":
			if strings.TrimSpace(r.SecretName) == "" {
				return errors.New("--secret-name is required")
			}
		case "file":
			if r.File == "" {
				return errors.New("--file is required")
			}
		case "yes":
			if !r.Yes {
				return errors.New("operation requires explicit --yes confirmation")
			}
		case "phrase-output":
			if r.PhraseOutput == "" {
				return errors.New("--phrase-output is required; phrase is never printed to stdout")
			}
		case "reveal":
			if !r.Reveal && r.Output == "" {
				return errors.New("secret output requires --reveal or --output")
			}
		case "ttl":
			if r.TTL < time.Second || r.TTL > 5*time.Minute {
				return errors.New("--ttl must be between 1s and 5m")
			}
		case "type":
			if r.Type != "password" && r.Type != "auth_code" {
				return errors.New("--type must be password or auth_code; use note create for notes")
			}
		}
	}
	if r.Command == "secret create" && r.Service == "" {
		return errors.New("--service is required")
	}
	if r.Generate && r.Type != "password" {
		return errors.New("generation is only available for passwords")
	}
	if r.Generate || r.Command == "password generate" {
		if err := passwordgen.Validate(r.Generator); err != nil {
			return err
		}
	} else {
		for _, k := range []string{"length", "lower", "upper", "digits", "special"} {
			if r.set[k] {
				return errors.New("generator options require --generate")
			}
		}
	}
	if r.Command == "note move" && !r.set["service"] && !r.set["folder"] {
		return errors.New("specify --service or --folder (empty means root)")
	}
	if r.Command == "note move" || r.Command == "note create" {
		if r.Service != "" && r.Folder != "" {
			return errors.New("choose a service or a folder for the note")
		}
	}
	if (r.Command == "folder move" && !r.set["parent"]) || ((r.Command == "service move" || r.Command == "secret move") && !r.set["folder"]) {
		return errors.New("specify the destination explicitly; an empty ID means root")
	}
	return nil
}
