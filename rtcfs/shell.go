package rtcfs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/binzume/webrtcfs/socfs"
)

func shellListFiles(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	printInfo := func(d fs.DirEntry, path string) {
		finfo, _ := d.Info()
		ent := socfs.NewFileEntry(finfo, true)
		fmt.Println(ent.Mode(), "\t", ent.Size(), "\t", ent.Type, "\t", path)
	}
	fpath := path.Join(cwd, arg)
	if strings.HasSuffix(fpath, "/**") {
		return fs.WalkDir(fsys, strings.TrimSuffix(fpath, "/**"), func(path string, d fs.DirEntry, err error) error {
			printInfo(d, path)
			return err
		})
	} else if fsys, ok := fsys.(socfs.OpenDirFS); ok {
		dir, err := fsys.OpenDir(fpath)
		if err != nil {
			return err
		}
		for {
			files, err := dir.ReadDir(200)
			for _, f := range files {
				printInfo(f, f.Name())
			}
			if err != nil {
				if err == io.EOF {
					err = nil
				}
				return err
			}
		}
	} else if fsys, ok := fsys.(fs.ReadDirFS); ok {
		files, err := fsys.ReadDir(fpath)
		for _, f := range files {
			printInfo(f, f.Name())
		}
		return err
	} else {
		return errors.New("not implemented")
	}
}

func shellCat(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	fpath := path.Join(cwd, arg)
	r, err := fsys.Open(fpath)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(os.Stdout, r)
	return err
}

func shellPullFile(ctx context.Context, fsys fs.FS, cwd, arg string) error {
	fpath := path.Join(cwd, arg)
	stat, err := fs.Stat(fsys, fpath)
	if err != nil {
		return err
	}
	log.Println("Pull: ", fpath, " (", stat.Size(), "B)")
	r, err := fsys.Open(fpath)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(filepath.Base(fpath))
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func shellPushFile(ctx context.Context, fsys *socfs.FSClient, cwd, arg string) error {
	r, err := os.Open(arg)
	if err != nil {
		return err
	}
	defer r.Close()
	stat, err := r.Stat()
	if err != nil {
		return err
	}
	log.Println("Push: ", arg, " (", stat.Size(), "B)")

	fpath := path.Join(cwd, filepath.Base(arg))
	w, err := fsys.Create(fpath)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = io.Copy(w, r)
	return err
}

func shellExecCmd(ctx context.Context, client *socfs.FSClient, cwd, cmd, arg string) error {
	switch cmd {
	case "":
		return nil
	case "pwd":
		fmt.Println(cwd)
		return nil
	case "ls":
		return shellListFiles(ctx, client, cwd, arg)
	case "pull":
		return shellPullFile(ctx, client, cwd, arg)
	case "cat":
		return shellCat(ctx, client, cwd, arg)
	case "push":
		return shellPushFile(ctx, client, cwd, arg)
	case "rm":
		return client.Remove(path.Join(cwd, arg))
	case "mkdir":
		return client.Mkdir(path.Join(cwd, arg), fs.ModePerm)
	case "?", "help":
		fmt.Println("Commands: exit, pwd, cd PATH, ls PATH, pull FILE, push FILE, cat FILE, rm FILE")
		return nil
	default:
		return errors.New("No such command: " + cmd)
	}
}

func ShellExec(ctx context.Context, options *ConnectOptions, cmd, arg string) error {
	rtcConn, client, err := GetClinet(ctx, options, &ClientOptions{MaxRedirect: 3})
	if err != nil {
		return err
	}
	defer rtcConn.Close()
	return shellExecCmd(ctx, client, "/", cmd, arg)
}

func StartShell(ctx context.Context, options *ConnectOptions) error {
	rtcConn, client, err := GetClinet(ctx, options, &ClientOptions{MaxRedirect: 3})
	if err != nil {
		return err
	}
	defer rtcConn.Close()

	cwd := "/"
	shellExecCmd(ctx, client, cwd, "help", "")
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		cmd := strings.Fields(s.Text())
		if len(cmd) == 0 {
			continue
		}
		arg := ""
		if len(cmd) > 1 {
			arg = cmd[1]
		}
		if cmd[0] == "exit" {
			return nil
		} else if cmd[0] == "cd" {
			cwd = path.Join(cwd, arg)
		} else {
			err := shellExecCmd(ctx, client, cwd, cmd[0], arg)
			if err != nil {
				fmt.Println("ERROR: ", err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
	return nil
}
