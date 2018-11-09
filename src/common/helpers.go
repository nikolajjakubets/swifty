package xh

import (
	"crypto/sha256"
	"encoding/hex"
	"gopkg.in/yaml.v2"
	"path/filepath"
	"crypto/rand"
	"io/ioutil"
	"syscall"
	"os/exec"
	"strconv"
	"strings"
	"bytes"
	"net"
	"fmt"
	"os"
)

func MakeEndpoint(addr string) string {
	if !(strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://")) {
		addr = "https://" + addr
	}
	return addr
}

func Sha256sum(s []byte) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func Cookify(val string) string {
	h := sha256.New()
	h.Write([]byte(val))
	return hex.EncodeToString(h.Sum(nil))
}

func CookifyS(vals ...string) string {
	h := sha256.New()
	for _, v := range vals {
		h.Write([]byte(v + "::"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var Letters = []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789")

func GenRandId(length int) (string, error) {
	idx := make([]byte, length)
	pass:= make([]byte, length)
	_, err := rand.Read(idx)
	if err != nil {
		return "", err
	}

	for i, j := range idx {
		pass[i] = Letters[int(j) % len(Letters)]
	}

	return string(pass), nil
}

func SafeEnv(env_name string, defaul_value string) string {
	v, found := os.LookupEnv(env_name)
	if found == false {
		return defaul_value
	}
	return v
}

func ReadYamlConfig(path string, c interface{}) error {
	yamlFile, err := ioutil.ReadFile(path)
	if err == nil {
		err = yaml.Unmarshal(yamlFile, c)
	}
	return err
}

func WriteYamlConfig(path string, c interface{}) error {
	bytes, err := yaml.Marshal(c)
	if err == nil {
		err = ioutil.WriteFile(path, bytes, 0600)
	}
	return err

}

// "ip:port" or ":port" expected
func GetIPPort(str string) (string, int32) {
	var port int = int(-1)
	var ip string = ""

	v := strings.Split(str, ":")
	if len(v) == 1 {
		port, _ = strconv.Atoi(v[0])
	} else if len(v) == 2 {
		port, _ = strconv.Atoi(v[1])
		ip = v[0]
	}
	return ip, int32(port)
}

func MakeIPPort(ip string, port int32) string {
	str := strconv.Itoa(int(port))
	return ip + ":" + str
}

func Exec(exe string, args []string) (bytes.Buffer, bytes.Buffer, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var cmd *exec.Cmd
	var err error

	cmd = exec.Command(exe, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return stdout, stderr, fmt.Errorf("runCmd: %s", err.Error())
	}

	return stdout, stderr, nil
}

func DropDir(dir, subdir string) (string, error) {
	nn, err := DropDirPrep(dir, subdir)
	if err != nil {
		return "", err
	}

	DropDirComplete(nn)
	return nn, nil
}

func DropDirPrep(dir, subdir string) (string, error) {
	_, err := os.Stat(dir + "/" + subdir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", fmt.Errorf("Can't stat %s%s: %s", dir, subdir, err.Error())
	}

	nname, err := ioutil.TempDir(dir, ".rm")
	if err != nil {
		return "", fmt.Errorf("leaking %s: %s", subdir, err.Error())
	}

	err = os.Rename(dir + "/" + subdir, nname + "/" + strings.Replace(subdir, "/", "_", -1))
	if err != nil {
		return "", fmt.Errorf("can't move repo clone: %s", err.Error())
	}

	return nname, nil
}

func DropDirComplete(nname string) {
	go os.RemoveAll(nname)
}

type XCreds struct {
	User    string
	Pass    string
	Host    string
	Port    string
	Domn	string
}

func (xc *XCreds)Addr() string {
	return xc.Host + ":" + xc.Port
}

func (xc *XCreds)AddrP(port string) string {
	return xc.Host + ":" + port
}

func (xc *XCreds)URL() string {
	s := xc.User + ":" + xc.Pass + "@" + xc.Host + ":" + xc.Port
	if xc.Domn != "" {
		s += "/" + xc.Domn
	}
	return s
}

func (xc *XCreds)Resolve() {
	if net.ParseIP(xc.Host) == nil {
		ips, err := net.LookupIP(xc.Host)
		if err == nil && len(ips) > 0 {
			xc.Host = ips[0].String()
		}
	}
}

func ParseXCreds(url string) *XCreds {
	xc := &XCreds{}
	/* user:pass@host:port */
	x := strings.SplitN(url, ":", 2)
	xc.User = x[0]
	x = strings.SplitN(x[1], "@", 2)
	xc.Pass = x[0]
	x = strings.SplitN(x[1], ":", 2)
	xc.Host = x[0]
	x = strings.SplitN(x[1], "/", 2)
	xc.Port = x[0]
	if len(x) > 1 {
		xc.Domn = x[1]
	}

	return xc
}

func Fortune() string {
	var fort []byte
	fort, err := exec.Command("fortune", "fortunes").Output()
	if err == nil {
		return string(fort)
	} else {
		return ""
	}
}

func GetLines(data []byte) []string {
        sout := strings.TrimSpace(string(data))
        return strings.Split(sout, "\n")
}

func GetDirDU(dir string) (uint64, error) {
	var size uint64

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		stat, _ := info.Sys().(*syscall.Stat_t)
		size += uint64(stat.Blocks << 9)
		return nil
	})

	return size, err
}
