package main

import (
	"path/filepath"
	"os/exec"
	"strings"
	"../apis"
)

type rt_info struct {
	CodePath	string
	Ext		string
	Build		bool
	Devel		bool
	BuildIP		string
	Version		string
	VArgs		[]string
	Packages	[]string
	PList		func() []string
}

func GetLines(lng string, args ...string) []string {
	cmd := append([]string{"run", "--rm", rtLangImage(lng)}, args...)
	out, err := exec.Command("docker", cmd...).Output()
	if err != nil {
		return []string{}
	}

	sout := strings.TrimSpace(string(out))
	return strings.Split(sout, "\n")
}

var py_info = rt_info {
	Ext:		"py",
	CodePath:	"/function",
	VArgs:		[]string{"python3", "--version"},
	PList:		func() []string {
		return GetLines("python", "pip3", "list", "--format", "freeze")
	},
}

var golang_info = rt_info {
	Ext:		"go",
	CodePath:	"/go/src/swycode",
	Build:		true,
	VArgs:		[]string{"go", "version"},
}

var swift_info = rt_info {
	Ext:		"swift",
	CodePath:	"/swift/swycode",
	Build:		true,
	VArgs:		[]string{"swift", "--version"},
}

var nodejs_info = rt_info {
	Ext:		"js",
	CodePath:	"/function",
	VArgs:		[]string{"node", "--version"},
	PList:		func() []string {
		o := GetLines("nodejs", "npm", "list")
		ret := []string{}
		if len(o) > 0 {
			for _, p := range(o[1:]) {
				ps := strings.Fields(p)
				ret = append(ret, ps[len(ps)-1])
			}
		}
		return ret
	},
}

var ruby_info = rt_info {
	Ext:		"rb",
	CodePath:	"/function",
	VArgs:		[]string{"ruby", "--version"},
	PList:		func() []string {
		return GetLines("ruby", "gem", "list")
	},
}

var rt_handlers = map[string]*rt_info {
	"python":	&py_info,
	"golang":	&golang_info,
	"swift":	&swift_info,
	"nodejs":	&nodejs_info,
	"ruby":		&ruby_info,
}
var extmap map[string]string

func init() {
	extmap = make(map[string]string)
	for l, d := range rt_handlers {
		extmap["." + d.Ext] = l
	}
}

func RtInit() {
	glog.Debugf("Will detect rt languages in the background")
	go func() {
		for l, h := range rt_handlers {
			args := append([]string{"run", "--rm", rtLangImage(l)}, h.VArgs...)
			out, err := exec.Command("docker", args...).Output()
			if err != nil {
				glog.Debugf("Cannot detect %s version", l)
				continue
			}

			h.Version = string(out)
		}
	}()
	go func() {
		for _, h := range rt_handlers {
			if h.PList == nil {
				continue
			}

			h.Packages = h.PList()
		}
	}()
}

func rtLangImage(lng string) string {
	return conf.Wdog.ImgPref + "/" + lng
}

func rtLangDetect(fname string) string {
	e := filepath.Ext(fname)
	return extmap[e]
}

func rtLangEnabled(lang string) bool {
	h, ok := rt_handlers[lang]
	return ok && (ModeDevel || !h.Devel)
}

func rtNeedToBuild(scr *FnCodeDesc) (bool, string) {
	rh := rt_handlers[scr.Lang]
	return rh.Build, rh.BuildIP
}

func rtSetBuilder(lang, ip string) {
	rt_handlers[lang].BuildIP = ip
}

/* Path where the sources would appear in container */
func rtCodePath(scr *FnCodeDesc) string {
	return rt_handlers[scr.Lang].CodePath
}

func rtScriptName(scr *FnCodeDesc, suff string) string {
	/* This should be in sync with wdog's startQnR and builders */
	return "script" + suff + "." + rt_handlers[scr.Lang].Ext
}

func (lh *rt_info)info() *swyapi.LangInfo {
	return &swyapi.LangInfo{
		Version:	lh.Version,
		Packages:	lh.Packages,
	}
}
