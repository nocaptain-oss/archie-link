//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/ipc"
	"golang.zx2c4.com/wireguard/tun"
)

const (
	name = "archie0"
	mtu  = 1280
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	tdev, err := newFDDevice(0)
	if err != nil {
		return fmt.Errorf("create TUN: %w", err)
	}
	logger := device.NewLogger(device.LogLevelVerbose, "("+name+") ")
	dev := device.NewDevice(tdev, conn.NewDefaultBind(), logger)
	defer dev.Close()

	uapiFile, err := ipc.UAPIOpen(name)
	if err != nil {
		return fmt.Errorf("open UAPI: %w", err)
	}
	uapi, err := ipc.UAPIListen(name, uapiFile)
	if err != nil {
		uapiFile.Close()
		return fmt.Errorf("listen UAPI: %w", err)
	}
	defer uapi.Close()

	term := make(chan os.Signal, 1)
	signal.Notify(term, unix.SIGTERM, os.Interrupt)
	defer signal.Stop(term)
	errs := make(chan error, 1)
	go func() {
		for {
			socket, acceptErr := uapi.Accept()
			if acceptErr != nil {
				errs <- acceptErr
				return
			}
			go dev.IpcHandle(socket)
		}
	}()

	select {
	case <-term:
		return nil
	case acceptErr := <-errs:
		if errors.Is(acceptErr, io.EOF) {
			return nil
		}
		return fmt.Errorf("UAPI accept: %w", acceptErr)
	case <-dev.Wait():
		return nil
	}
}

type fdDevice struct {
	file       *os.File
	events     chan tun.Event
	cancelPipe [2]int
	closeOnce  sync.Once
}

func newFDDevice(fd int) (*fdDevice, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	pipe := [2]int{-1, -1}
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, err
	}
	events := make(chan tun.Event, 1)
	events <- tun.EventUp
	return &fdDevice{
		file:       os.NewFile(uintptr(fd), "tun"),
		events:     events,
		cancelPipe: pipe,
	}, nil
}

func (d *fdDevice) File() *os.File {
	return d.file
}

func (d *fdDevice) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if len(bufs) == 0 || len(sizes) == 0 || offset > len(bufs[0]) {
		return 0, io.ErrShortBuffer
	}
	fds := []unix.PollFd{
		{Fd: int32(d.file.Fd()), Events: unix.POLLIN},
		{Fd: int32(d.cancelPipe[0]), Events: unix.POLLIN},
	}
	for {
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, err
		}
		if fds[1].Revents != 0 {
			return 0, io.EOF
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return 0, io.EOF
		}
		if fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		size, err := unix.Read(int(d.file.Fd()), bufs[0][offset:])
		if errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return 0, err
		}
		sizes[0] = size
		return 1, nil
	}
}

func (d *fdDevice) Write(bufs [][]byte, offset int) (int, error) {
	if len(bufs) == 0 || offset > len(bufs[0]) {
		return 0, io.ErrShortBuffer
	}
	payload := bufs[0][offset:]
	for {
		written, err := unix.Write(int(d.file.Fd()), payload)
		if errors.Is(err, unix.EAGAIN) {
			poll := []unix.PollFd{{Fd: int32(d.file.Fd()), Events: unix.POLLOUT}}
			if _, pollErr := unix.Poll(poll, -1); pollErr != nil {
				return 0, pollErr
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		if written != len(payload) {
			return 0, io.ErrShortWrite
		}
		return 1, nil
	}
}

func (d *fdDevice) MTU() (int, error) {
	return mtu, nil
}

func (d *fdDevice) Name() (string, error) {
	return name, nil
}

func (d *fdDevice) Events() <-chan tun.Event {
	return d.events
}

func (d *fdDevice) BatchSize() int {
	return 1
}

func (d *fdDevice) Close() error {
	var closeErr error
	d.closeOnce.Do(func() {
		_, _ = unix.Write(d.cancelPipe[1], []byte{1})
		closeErr = d.file.Close()
		_ = unix.Close(d.cancelPipe[0])
		_ = unix.Close(d.cancelPipe[1])
		close(d.events)
	})
	return closeErr
}
