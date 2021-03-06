package ssh

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hsyan2008/hfw"
	"github.com/hsyan2008/hfw/pac"
)

type ProxyIni struct {
	Bind string `toml:"bind"`
	//区分是http还是socks5
	IsHTTP bool `toml:"is_http"`
	//是否所有请求通过ssh访问
	IsSSH bool `toml:"is_ssh"`
	//是否根据pac决定是否通过ssh访问，如果IsSSH=true，此配置无效
	IsPac bool `toml:"is_pac"`
	//如果不在pac列表里，是否中断，IsPac=true才生效
	IsBreak bool `toml:"is_break"`
}
type Proxy struct {
	httpCtx  *hfw.HTTPContext
	pi       *ProxyIni
	c        *SSH
	listener net.Listener
}

func NewProxy(httpCtx *hfw.HTTPContext, sshConfig SSHConfig, pi *ProxyIni) (p *Proxy, err error) {
	if pi == nil || pi.Bind == "" {
		return nil, errors.New("err ini")
	}
	if !strings.Contains(pi.Bind, ":") {
		pi.Bind = ":" + pi.Bind
	}
	if pi.IsPac {
		err = pac.LoadDefault()
		if err != nil {
			return
		}
	}
	if httpCtx == nil {
		httpCtx = hfw.NewHTTPContext()
	}
	p = &Proxy{
		pi:      pi,
		httpCtx: httpCtx,
	}

	p.c, err = NewSSH(sshConfig)

	if err == nil {
		err = p.Bind()
		if err == nil {
			p.httpCtx.Infof("Bind %s for proxy success, start to accept", p.listener.Addr().String())
			go p.Accept()
		}
	}

	return
}

func (p *Proxy) Listener() net.Listener {
	return p.listener
}

func (p *Proxy) SSH() *SSH {
	return p.c
}

func (p *Proxy) Bind() (err error) {
	p.listener, err = net.Listen("tcp", p.pi.Bind)
	return
}
func (p *Proxy) Accept() {
	for {
		select {
		case <-p.httpCtx.Ctx.Done():
			return
		default:
			conn, err := p.listener.Accept()
			if err != nil {
				p.Close()
				if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
					p.httpCtx.Error(err)
				}
				return
			}

			go func() {
				if p.pi.IsHTTP {
					_ = p.HandHTTP(conn)
				} else {
					_ = p.HandSocks5(conn)
				}
			}()
		}
	}
}

func (p *Proxy) HandHTTP(conn net.Conn) (err error) {

	r := bufio.NewReader(conn)

	req, err := http.ReadRequest(r)
	if err != nil {
		_ = conn.Close()
		return
	}

	req.Header.Del("Proxy-Connection")
	//否则远程连接不会关闭，导致Copy卡住
	req.Header.Set("Connection", "close")

	p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(req.Host), req.Host, "connecting...")
	con, err := p.dial(req.Host)
	if err != nil {
		p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(req.Host), req.Host, "connected faild", err)
		_ = conn.Close()
		return
	}
	p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(req.Host), req.Host, "connected.")
	if req.Method == "CONNECT" {
		_, err = io.WriteString(conn, "HTTP/1.0 200 Connection Established\r\n\r\n")
	} else {
		err = req.Write(con)
	}
	if err != nil {
		_ = conn.Close()
		_ = con.Close()
		return
	}

	go multiCopy(conn, con)
	go multiCopy(con, conn)

	return
}
func (p *Proxy) HandSocks5(conn net.Conn) (err error) {

	var buf []byte

	//client发送请求来协商版本和认证方法
	buf, err = readLen(conn, 1+1+255)
	if err != nil {
		_ = conn.Close()
		return
	}

	//暂时只支持V5
	if buf[0] != 0x05 {
		_ = conn.Close()
		return
	}

	//回应版本和认证方法
	_, err = conn.Write([]byte{0x05, 0x00})
	if err != nil {
		_ = conn.Close()
		return
	}

	//请求目标地址
	buf, err = readLen(conn, 4)
	if err != nil {
		_ = conn.Close()
		return
	}
	cmd := buf[1]
	switch cmd {
	case 0x01: //tcp
	case 0x02: //bind不支持
		fallthrough
	case 0x03: //udp不支持
		_, _ = conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		_ = conn.Close()
		return
	}
	atyp := buf[3]
	var host string
	var port uint16
	buf, err = readLen(conn, 1024)
	if err != nil {
		_ = conn.Close()
		return
	}
	switch atyp {
	case 0x01: //ipv4地址，php代码可以测试
		host = net.IP(buf[:4]).String()
	case 0x03: //域名，firefox浏览器下可以测试
		host = string(buf[1 : len(buf)-2])
	case 0x04: //ipv6地址不支持
		_, _ = conn.Write([]byte{0x05, 0x02, 0x00, atyp, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		_ = conn.Close()
		return
	}
	err = binary.Read(bytes.NewReader(buf[len(buf)-2:]), binary.BigEndian, &port)
	if err != nil {
		_ = conn.Close()
		return
	}

	p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(host), host, "connecting...")
	host = host + ":" + strconv.Itoa(int(port))
	con, err := p.dial(host)
	if err != nil {
		// _, _ = conn.Write([]byte{0x05, 0x06, 0x00, atyp})
		_ = conn.Close()
		p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(host), host, "connected faild", err)
		return
	}
	p.httpCtx.Info(p.listener.Addr().String(), conn.RemoteAddr().String(), p.isSSH(host), host, "connected.")

	_, err = conn.Write([]byte{0x05, 0x00, 0x00, atyp})
	if err != nil {
		_ = conn.Close()
		_ = con.Close()
		return
	}
	//把地址写回去
	_, err = conn.Write(buf)
	if err != nil {
		_ = conn.Close()
		_ = con.Close()
		return
	}

	go multiCopy(con, conn)
	go multiCopy(conn, con)

	return
}
func readLen(conn net.Conn, len int) (buf []byte, err error) {
	buf = make([]byte, len)
	var n int

	n, err = conn.Read(buf)
	if err != nil {
		return
	}

	return buf[:n], nil
}

func (p *Proxy) isSSH(addr string) bool {
	if p.pi.IsSSH == false {
		if p.pi.IsPac {
			return pac.Check(addr)
		} else {
			return false
		}
	}

	return true
}

func (p *Proxy) Close() {
	p.httpCtx.Cancel()
	_ = p.listener.Close()
	p.c.Close()
}

func (p *Proxy) dial(addr string) (con net.Conn, err error) {
	isSSH := p.isSSH(addr)
	if !isSSH && p.pi.IsPac && p.pi.IsBreak {
		return nil, errors.New("不在Pac名单")
	}
	if strings.HasPrefix(addr, "[") && strings.HasSuffix(addr, "]") ||
		!strings.Contains(addr, ":") {
		addr = fmt.Sprintf("%s:80", addr)
	}
	if isSSH {
		con, err = p.c.Connect(addr)
	} else {
		con, err = net.DialTimeout("tcp", addr, 30*time.Second)
	}

	return
}
