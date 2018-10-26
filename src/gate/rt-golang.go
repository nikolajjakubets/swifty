package main

import (
	"os/exec"
	"os"
	"bytes"
	"strings"
	"errors"
	"context"
	"swifty/common"
)

const (
	goOsArch string = "linux_amd64" /* FIXME -- run go env and parse */
)

var golang_info = langInfo {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
	VArgs:		[]string{"go", "version"},

	Install:	goInstall,
	Remove:		goRemove,
	BuildPkgPath:	goPkgPath,
}

func goInstall(ctx context.Context, id SwoId) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if strings.Contains(id.Name, "...") {
		return errors.New("No wildcards (at least yet)")
	}

	tgt_dir := packagesDir() + "/" + id.Tennant + "/golang"
	os.MkdirAll(tgt_dir, 0755)
	args := []string{"run", "--rm", "-v", tgt_dir + ":/go", rtLangImage("golang"), "go", "get", id.Name}
	ctxlog(ctx).Debugf("Running docker %v", args)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		logSaveResult(ctx, id.PCookie(), "pkg_install", stdout.String(), stderr.String())
		return errors.New("Error installing pkg")
	}

	return nil
}

func goRemove(ctx context.Context, id SwoId) error {
	d := packagesDir() + "/" + id.Tennant + "/golang"
	err := os.Remove(d + "/pkg/" + goOsArch + "/" + id.Name + ".a")
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove %s' package: %s", id.Str(), err.Error())
		return errors.New("Error removing pkg")
	}

	x, err := xh.DropDir(d, "src/" + id.Name)
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove %s' sources (%s): %s", id.Str(), x, err.Error())
		return errors.New("Error removing pkg")
	}

	return nil
}

func goPkgPath(id SwoId) string {
	return "/go-pkg/" + id.Tennant + "/golang"
}
