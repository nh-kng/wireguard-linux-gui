package autostart

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

const helperArgument = "--autostart-helper"

type request struct {
	ConfigDir string
	Write     *Snapshot
}
type response struct {
	Snapshot Snapshot
	Error    string
}

// RunHelper must run before GTK initialization. Only unprivileged children may
// service requests, so user-controlled paths are never accessed as root.
func RunHelper(args []string) (bool, error) {
	if len(args) < 2 || args[1] != helperArgument {
		return false, nil
	}
	if len(args) != 2 || os.Geteuid() == 0 {
		return true, errors.New("autostart helper requires an unprivileged user")
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&req); err != nil {
		return true, err
	}
	store := Store{ConfigDir: req.ConfigDir}
	var result response
	var err error
	if req.Write == nil {
		result.Snapshot, err = store.Read()
	} else {
		err = store.Write(*req.Write)
	}
	if err != nil {
		result.Error = err.Error()
	}
	return true, json.NewEncoder(os.Stdout).Encode(result)
}

type Client struct {
	configDir, executable, home, username string
	credential                            *syscall.Credential
}

func NewClient(uid int, config string) (*Client, error) {
	if uid <= 0 {
		return nil, errors.New("start at login requires a non-root desktop user")
	}
	account, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return nil, err
	}
	if config == "" {
		config, err = ConfigDir(account.HomeDir, "")
	}
	if err != nil {
		return nil, err
	}
	if _, err = (Store{ConfigDir: config}).path(); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	client := &Client{configDir: config, executable: executable, home: account.HomeDir, username: account.Username}
	if os.Geteuid() == 0 {
		gid, err := strconv.ParseUint(account.Gid, 10, 32)
		if err != nil {
			return nil, err
		}
		groups, err := account.GroupIds()
		if err != nil {
			return nil, err
		}
		credential := &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
		for _, group := range groups {
			id, err := strconv.ParseUint(group, 10, 32)
			if err != nil {
				return nil, err
			}
			credential.Groups = append(credential.Groups, uint32(id))
		}
		client.credential = credential
	} else if os.Geteuid() != uid {
		return nil, errors.New("desktop user does not match process user")
	}
	return client, nil
}

func (c *Client) call(write *Snapshot) (Snapshot, error) {
	data, err := json.Marshal(request{ConfigDir: c.configDir, Write: write})
	if err != nil {
		return Snapshot{}, err
	}
	command := exec.Command(c.executable, helperArgument)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: c.credential}
	command.Env = []string{"HOME=" + c.home, "USER=" + c.username, "PATH=/usr/bin:/bin"}
	command.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("autostart helper: %w: %s", err, stderr.String())
	}
	var result response
	if err := json.Unmarshal(output, &result); err != nil {
		return Snapshot{}, err
	}
	if result.Error != "" {
		return Snapshot{}, errors.New(result.Error)
	}
	return result.Snapshot, nil
}
func (c *Client) Read() (Snapshot, error)       { return c.call(nil) }
func (c *Client) Write(snapshot Snapshot) error { _, err := c.call(&snapshot); return err }
