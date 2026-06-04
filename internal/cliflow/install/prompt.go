package install

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type Prompt struct {
	in  io.Reader
	out io.Writer
}

type PromptOptions struct {
	PanelAccess    string
	PanelAccessSet bool
	Domain         string
	Email          string
	PanelPort      int
	PanelPortSet   bool
}

func NewPrompt(in io.Reader, out io.Writer) Prompt {
	return Prompt{in: in, out: out}
}

func (p Prompt) PromptMissingOptions(opts *PromptOptions) error {
	reader := bufio.NewReader(p.in)
	domainPattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)
	if !opts.PanelAccessSet {
		for {
			fmt.Fprint(p.out, "Panel access mode [local/direct/caddy] (default local): ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			candidate := strings.ToLower(strings.TrimSpace(value))
			if candidate == "" {
				candidate = "local"
			}
			if candidate == "local" || candidate == "direct" || candidate == "caddy" {
				opts.PanelAccess = candidate
				break
			}
			fmt.Fprintln(p.out, "Panel access mode must be local, direct, or caddy.")
		}
	}
	if !opts.PanelPortSet {
		for {
			fmt.Fprint(p.out, "Panel port mode [random/custom] (default random): ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			answer := strings.ToLower(strings.TrimSpace(value))
			if answer == "" || answer == "r" || answer == "random" || answer == "n" || answer == "no" {
				opts.PanelPort = 0
				break
			}
			if answer == "c" || answer == "custom" || answer == "y" || answer == "yes" {
				port, err := p.promptPanelPort(reader)
				if err != nil {
					return err
				}
				opts.PanelPort = port
				break
			}
			fmt.Fprintln(p.out, "Panel port mode must be random or custom.")
		}
	}
	if opts.PanelAccess == "caddy" {
		if strings.TrimSpace(opts.Domain) == "" {
			for {
				fmt.Fprint(p.out, "Domain for Veil/ACME: ")
				value, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				candidate := strings.TrimSpace(value)
				if candidate == "" {
					fmt.Fprintln(p.out, "Domain must not be empty.")
					continue
				}
				if !domainPattern.MatchString(candidate) {
					fmt.Fprintln(p.out, "Domain must be a valid domain name (e.g. example.com).")
					continue
				}
				opts.Domain = candidate
				break
			}
		}
		if strings.TrimSpace(opts.Email) == "" {
			fmt.Fprint(p.out, "ACME email: ")
			value, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			opts.Email = strings.TrimSpace(value)
		}
	}
	return nil
}

func (p Prompt) promptPanelPort(reader *bufio.Reader) (int, error) {
	for {
		fmt.Fprint(p.out, "Panel TCP port: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			fmt.Fprintln(p.out, "Port must be a number between 1 and 65535.")
			continue
		}
		if parsed < 1 || parsed > 65535 {
			fmt.Fprintln(p.out, "Port must be between 1 and 65535.")
			continue
		}
		return parsed, nil
	}
}
