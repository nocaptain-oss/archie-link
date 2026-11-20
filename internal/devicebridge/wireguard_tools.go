//go:build tools

package devicebridge

import (
	_ "golang.zx2c4.com/wireguard/conn"
	_ "golang.zx2c4.com/wireguard/device"
	_ "golang.zx2c4.com/wireguard/ipc"
	_ "golang.zx2c4.com/wireguard/tun"
)
