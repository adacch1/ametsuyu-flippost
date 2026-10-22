//go:build linux

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
)

func nlaPut(typ uint16, p []byte) []byte {
	l := 4 + len(p)
	b := make([]byte, align4(l))
	binary.LittleEndian.PutUint16(b[0:], uint16(l))
	binary.LittleEndian.PutUint16(b[2:], typ)
	copy(b[4:], p)
	return b
}

// genlRequest builds a netlink message with a generic netlink header.
func genlRequest(typ, flags uint16, cmd byte, attrs []byte) []byte {
	b := make([]byte, 20, 20+len(attrs))
	binary.LittleEndian.PutUint16(b[4:], typ)
	binary.LittleEndian.PutUint16(b[6:], flags)
	binary.LittleEndian.PutUint32(b[8:], 1)
	b[16], b[17] = cmd, 1
	b = append(b, attrs...)
	binary.LittleEndian.PutUint32(b[0:], uint32(len(b)))
	return b
}

func openGenl() (int, error) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_GENERIC)
	if err != nil {
		return -1, err
	}
	tv := syscall.Timeval{Sec: 2}
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		syscall.Close(fd)
		return -1, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// readUntilDone concatenates reads until a message of type NLMSG_DONE or NLMSG_ERROR arrives.
func readUntilDone(fd int) ([]byte, error) {
	var all []byte
	buf := make([]byte, 64<<10)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return nil, err
		}
		chunk := buf[:n]
		all = append(all, chunk...)
		for len(chunk) >= 16 {
			l := int(binary.LittleEndian.Uint32(chunk[0:]))
			if l < 16 || l > len(chunk) {
				break
			}
			if t := binary.LittleEndian.Uint16(chunk[4:]); t == nlmsgDone || t == nlmsgError {
				return all, nil
			}
			chunk = chunk[min(align4(l), len(chunk)):]
		}
	}
}

// genlFamilyID resolves a generic netlink family name to its id.
func genlFamilyID(fd int, name string) (uint16, error) {
	req := genlRequest(genlIDCtrl, nlmFRequest, ctrlCmdGetFamily, nlaPut(2, append([]byte(name), 0)))
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return 0, err
	}
	buf := make([]byte, 64<<10)
	n, _, err := syscall.Recvfrom(fd, buf, 0)
	if err != nil {
		return 0, err
	}
	b := buf[:n]
	if n < 20 {
		return 0, errors.New("nl80211: short family reply")
	}
	if binary.LittleEndian.Uint16(b[4:]) == nlmsgError {
		return 0, fmt.Errorf("nl80211: family lookup: %w", syscall.Errno(-int32(binary.LittleEndian.Uint32(b[16:]))))
	}
	var id uint16
	err = eachAttr(b[20:n], func(typ uint16, v []byte) {
		if typ == 1 && len(v) >= 2 {
			id = binary.LittleEndian.Uint16(v)
		}
	})
	if err == nil && id == 0 {
		err = errors.New("nl80211: family id not found")
	}
	return id, err
}

// dumpStations lists the stations associated with iface.
func dumpStations(iface string) ([]Station, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	fd, err := openGenl()
	if err != nil {
		return nil, err
	}
	defer syscall.Close(fd)
	id, err := genlFamilyID(fd, "nl80211")
	if err != nil {
		return nil, err
	}
	idx := make([]byte, 4)
	binary.LittleEndian.PutUint32(idx, uint32(ifi.Index))
	req := genlRequest(id, nlmFRequest|nlmFDump, nl80211CmdGetStation, nlaPut(nl80211AttrIfindex, idx))
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil, err
	}
	all, err := readUntilDone(fd)
	if err != nil {
		return nil, err
	}
	return parseStations(all)
}
