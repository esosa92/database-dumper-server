package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/term"

	"database-dumper-server/db"
)

const usage = `usage:
  server                          run the web server
  server user list
  server user create <username> [--admin]
  server user passwd <username>
  server user admin <username> <on|off>
  server user delete <username>

Passwords are read from DUMPER_ADMIN_PASSWORD if set, otherwise prompted.`

func runCLI(args []string) error {
	if len(args) < 2 || args[0] != "user" {
		return errors.New(usage)
	}
	switch args[1] {
	case "list":
		users, err := db.ListUsers()
		if err != nil {
			return err
		}
		for _, u := range users {
			flag := ""
			if u.IsAdmin {
				flag = "  (admin)"
			}
			fmt.Printf("%d\t%s%s\n", u.ID, u.Username, flag)
		}
		return nil
	case "create":
		if len(args) < 3 {
			return errors.New(usage)
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		isAdmin := len(args) > 3 && args[3] == "--admin"
		u, err := db.CreateUser(args[2], pw, isAdmin)
		if err != nil {
			return err
		}
		fmt.Printf("created user %s (id %d)\n", u.Username, u.ID)
		return nil
	case "passwd":
		if len(args) < 3 {
			return errors.New(usage)
		}
		u, err := db.GetUserByUsername(args[2])
		if err != nil {
			return fmt.Errorf("user %q not found", args[2])
		}
		pw, err := readPassword()
		if err != nil {
			return err
		}
		if err := db.SetPassword(u.ID, pw); err != nil {
			return err
		}
		db.DeleteUserSessions(u.ID)
		fmt.Printf("password updated for %s\n", u.Username)
		return nil
	case "admin":
		if len(args) < 4 || (args[3] != "on" && args[3] != "off") {
			return errors.New(usage)
		}
		u, err := db.GetUserByUsername(args[2])
		if err != nil {
			return fmt.Errorf("user %q not found", args[2])
		}
		if err := db.UpdateUser(u.ID, u.Username, args[3] == "on"); err != nil {
			return err
		}
		fmt.Printf("admin=%s for %s\n", args[3], u.Username)
		return nil
	case "delete":
		if len(args) < 3 {
			return errors.New(usage)
		}
		u, err := db.GetUserByUsername(args[2])
		if err != nil {
			return fmt.Errorf("user %q not found", args[2])
		}
		if err := db.DeleteUser(u.ID); err != nil {
			return err
		}
		fmt.Printf("deleted user %s\n", u.Username)
		return nil
	}
	return errors.New(usage)
}

func readPassword() (string, error) {
	if pw := os.Getenv("DUMPER_ADMIN_PASSWORD"); pw != "" {
		return pw, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password provided: set DUMPER_ADMIN_PASSWORD or pipe it on stdin")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Print("Password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Repeat password: ")
	again, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(pw) != string(again) {
		return "", errors.New("passwords do not match")
	}
	return string(pw), nil
}

func bootstrapAdmin() error {
	n, err := db.CountUsers()
	if err != nil || n > 0 {
		return err
	}
	pw := os.Getenv("DUMPER_ADMIN_PASSWORD")
	generated := false
	if pw == "" {
		buf := make([]byte, 9)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		pw = hex.EncodeToString(buf)
		generated = true
	}
	if _, err := db.CreateUser("admin", pw, true); err != nil {
		return err
	}
	if generated {
		log.Printf("created initial user 'admin' with generated password: %s", pw)
	} else {
		log.Printf("created initial user 'admin' with password from DUMPER_ADMIN_PASSWORD")
	}
	return nil
}
