//go:build darwin

package deviceid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

const keychainExecutable = "/usr/bin/security"

type KeychainStore struct {
	Service      string
	KeychainPath string
}

func (s KeychainStore) Load(ctx context.Context, account string) (string, error) {
	service := s.Service
	if service == "" {
		service = "com.archielink.device"
	}
	args := []string{"find-generic-password", "-s", service, "-a", account, "-w"}
	if s.KeychainPath != "" {
		args = append(args, s.KeychainPath)
	}
	output, err := exec.CommandContext(ctx, keychainExecutable, args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "could not be found") {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("read device keychain item: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (s KeychainStore) Save(ctx context.Context, account, secret string) error {
	service := s.Service
	if service == "" {
		service = "com.archielink.device"
	}
	command := fmt.Sprintf(
		"add-generic-password -U -s %s -a %s -w %s",
		shellQuote(service), shellQuote(account), shellQuote(secret),
	)
	if s.KeychainPath != "" {
		command += " " + shellQuote(s.KeychainPath)
	}
	if len(command) > 4096 {
		return errors.New("device keychain command is too large")
	}

	cmd := exec.CommandContext(ctx, keychainExecutable, "-i")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := io.WriteString(stdin, command+"\n"); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return err
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("write device keychain item: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
