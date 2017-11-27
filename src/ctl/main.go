package main

import (
	"encoding/json"
	"encoding/base64"
	"path/filepath"
	"io/ioutil"
	"net/http"
	"strings"
	"regexp"
	"flag"
	"fmt"
	"os"

	"../common"
	"../apis/apps"
)

type LoginInfo struct {
	Proj		string		`yaml:"proj"`
	Host		string		`yaml:"host"`
	Port		string		`yaml:"port"`
	Token		string		`yaml:"token"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"pass"`
}

type YAMLConf struct {
	Login		LoginInfo	`yaml:"login"`
}

var conf YAMLConf

func make_faas_req_x(url string, in interface{}, succ_code int) (*http.Response, error) {
	var address string = "http://" + conf.Login.Host + ":" + conf.Login.Port + "/v1/" + url
	var cb swy.HTTPMarshalAndPostCB = func(r *http.Request) error {
			if conf.Login.Token != "" {
				r.Header.Set("X-Auth-Token",
						conf.Login.Token)
			}
			return nil
	}
	return swy.HTTPMarshalAndPost2(address, in, cb, succ_code)
}

func faas_login() string {
	url := "user/login"
	if conf.Login.Proj == "admin" {
		url = "login"
	}

	resp, err := make_faas_req_x(url, swyapi.UserLogin {
			UserName: conf.Login.User, Password: conf.Login.Pass,
		}, http.StatusOK)
	if err != nil {
		panic(err)
	}

	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		panic("No auth token from server")
	}

	return token
}

func make_faas_req(url string, in interface{}, out interface{}) {
	make_faas_req2(url, in, out, http.StatusOK)
}

func make_faas_req2(url string, in interface{}, out interface{}, succ_code int) {
	first_attempt := true
again:
	resp, err := make_faas_req_x(url, in, succ_code)
	if err != nil {
		if resp == nil {
			panic(err)
		}

		resp.Body.Close()
		if (resp.StatusCode == http.StatusUnauthorized) && first_attempt {
			first_attempt = false
			refresh_token("")
			goto again
		}

		panic(fmt.Errorf("Bad responce from server: " + string(resp.Status)))
	}

	/* Here we have http.StatusOK */
	defer resp.Body.Close()

	if out != nil {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		err = json.Unmarshal(body, out)
		if err != nil {
			panic(err)
		}
	}
}

func list_users() {
	var uss []swyapi.UserInfo
	make_faas_req("users", swyapi.ListUsers{}, &uss)

	for _, u := range uss {
		fmt.Printf("%s (%s)\n", u.Id, u.Name)
	}
}

func add_user(id, name, pass string) {
	make_faas_req2("adduser", swyapi.AddUser{Id: id, Pass:pass, Name: name},
		nil, http.StatusCreated)
}

func del_user(id string) {
	make_faas_req2("deluser", swyapi.UserInfo{Id: id}, nil, http.StatusNoContent)
}

func set_password(id, pass string) {
	make_faas_req2("setpass", swyapi.UserLogin{UserName: id, Password: pass},
		nil, http.StatusCreated)
}

func show_user_info(id string) {
	var ui swyapi.UserInfo
	make_faas_req("userinfo", swyapi.UserInfo{Id: id}, &ui)
	fmt.Printf("Name: %s\n", ui.Name)
}

func list_projects() {
	var ps []swyapi.ProjectItem
	make_faas_req("project/list", swyapi.ProjectList{}, &ps)

	for _, p := range ps {
		fmt.Printf("%s\n", p.Project)
	}
}

func list_functions(project string) {
	var fns []swyapi.FunctionItem
	make_faas_req("function/list", swyapi.FunctionList{ Project: project, }, &fns)

	fmt.Printf("%-20s%-10s\n", "NAME", "STATE")
	for _, fn := range fns {
		fmt.Printf("%-20s%-12s\n", fn.FuncName, fn.State)
	}
}

func info_function(project, name string) {
	var ifo swyapi.FunctionInfo
	make_faas_req("function/info", swyapi.FunctionID{ Project: project, FuncName: name}, &ifo)

	fmt.Printf("Lang:   %s\n", ifo.Code.Lang)
	fmt.Printf("Commit: %s\n", ifo.Commit[:8])
	fmt.Printf("State:  %s\n", ifo.State)
	if len(ifo.Mware) > 0 {
		fmt.Printf("Mware:  %s\n", strings.Join(ifo.Mware, ", "))
	}
	if ifo.Event.Source != "" {
		estr := ifo.Event.Source
		if ifo.Event.Source == "url" {
			/* nothing */
		} else if ifo.Event.CronTab != "" {
			estr += ":" + ifo.Event.CronTab
		} else if ifo.Event.MwareId != "" {
			estr += ":" + ifo.Event.MwareId
			if ifo.Event.MQueue != "" {
				estr += ":" + ifo.Event.MQueue
			}
		} else {
			estr += "UNKNOWN"
		}
		fmt.Printf("Event:  %s\n", estr)
	}
	if ifo.URL != "" {
		fmt.Printf("URL:    http://%s:%s%s\n", conf.Login.Host, conf.Login.Port, ifo.URL)
	}
	fmt.Printf("Called: %d\n", ifo.Stats.Called)
}

func detect_language(repo string) string {
	panic("can't detect function language")
}

func add_function(name, lang, src, mwares, event string) {
	sources := swyapi.FunctionSources{}
	code := swyapi.FunctionCode{}

	st, err := os.Stat(src)
	if err != nil {
		panic("Can't stat sources path")
	}

	if st.IsDir() {
		repo, err := filepath.Abs(src)
		if err != nil {
			panic("Can't get abs path for repo")
		}

		fmt.Printf("Will add git repo %s\n", repo)
		sources.Type = "git"
		sources.Repo = repo
	} else {
		data, err := ioutil.ReadFile(src)
		if err != nil {
			panic("Can't read file sources")
		}

		enc := base64.StdEncoding.EncodeToString(data)

		fmt.Printf("Will add file %s\n", src)
		sources.Type = "code"
		sources.Code = enc
	}

	if lang == "auto" {
		lang = detect_language(src)
	}

	code.Lang = lang


	mw := strings.Split(mwares, ",")

	evt := swyapi.FunctionEvent {}
	if event != "" {
		mwe := strings.Split(event, ":")
		evt.Source = mwe[0]
		if evt.Source == "url" {
			/* nothing */
		} else if evt.Source == "mware" {
			evt.MwareId = mwe[1]
			evt.MQueue = mwe[2]
		} else {
			/* FIXME -- CRONTAB */
			panic("Unknown event string")
		}
	}

	make_faas_req("function/add",
		swyapi.FunctionAdd{
			Project: conf.Login.Proj,
			FuncName: name,
			Sources: sources,
			Code: code,
			Mware: mw,
			Event: evt,
		}, nil)

}

func run_function(name string, args []string) {
	var rres swyapi.FunctionRunResult
	make_faas_req("function/run",
		swyapi.FunctionRun{ Project: conf.Login.Proj, FuncName: name, Args: args, }, &rres)

	fmt.Printf("code: %d\n", rres.Code)
	fmt.Printf("returned: %s\n", rres.Return)
	fmt.Printf("%s", rres.Stdout)
	fmt.Fprintf(os.Stderr, "%s", rres.Stderr)
}

func update_function(name string) {
	make_faas_req("function/update",
		swyapi.FunctionUpdate{ Project: conf.Login.Proj, FuncName: name, }, nil)

}

func del_function(name string) {
	make_faas_req("function/remove",
		swyapi.FunctionRemove{ Project: conf.Login.Proj, FuncName: name }, nil)
}

func show_logs(name string) {
	var res []swyapi.FunctionLogEntry
	make_faas_req("function/logs",
		swyapi.FunctionID{ Project: conf.Login.Proj, FuncName: name, }, &res)

	for _, le := range res {
		fmt.Printf("%s %s/%s: %s\n", le.Ts, le.Commit[:8], le.Event, le.Text)
	}
}

func list_mware(project string) {
	var mws []swyapi.MwareGetItem
	make_faas_req("mware/list", swyapi.MwareList{ Project: project, }, &mws)

	fmt.Printf("%-20s%-10s%s\n", "NAME", "ID", "OPTIONS")
	for _, mw := range mws {
		fmt.Printf("%-20s%-10s%s\n", mw.ID, mw.Type, mw.JSettings)
	}
}

func add_mware(mwares []string) {
	req := swyapi.MwareAdd { Project: conf.Login.Proj }

	for _, mw := range mwares {
		mws := strings.SplitN(mw, ":", 2)
		fmt.Printf("Will add %s (%s) mware\n", mws[1], mws[0])
		req.Mware = append(req.Mware, swyapi.MwareItem {
					Type: mws[0],
					ID: mws[1],
				})
	}

	make_faas_req("mware/add", req, nil)
}

func del_mware(mwares []string) {
	make_faas_req("mware/remove",
		swyapi.MwareRemove{ Project: conf.Login.Proj, MwareIDs: mwares, }, nil)
}

func show_mware_env(mwid string) {
	var r swyapi.MwareCinfoResp

	make_faas_req("mware/cinfo", swyapi.MwareCinfo { Project: conf.Login.Proj, MwId: mwid }, &r)
	for _, e := range r.Envs {
		fmt.Printf("%s\n", e)
	}
}

func login() {
	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	err := swy.ReadYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		fmt.Printf("Login first\n")
		os.Exit(1)
	}
}

func make_login(creds string) {
	//
	// Login string is user:pass@host:port/project
	//
	// swifty.user:swifty@10.94.96.216:8686/swifty.proj
	//
	var match string = `([(a-z)(A-Z)(0-9)\.\-\_]+)` + `:` +
			`([(a-z)(A-Z)(0-9)]+)` + `@` +
			`([(0-9)\.])+` + `:` +
			`([0-9])+` + `/` +
			`([(a-z)(A-Z)(0-9)\.\-\_]+)`

	home, found := os.LookupEnv("HOME")
	if !found {
		panic("No HOME dir set")
	}

	matched, err := regexp.MatchString(match, creds)
	if err != nil || matched == false {
		return
	}

	data := regexp.MustCompile(":|/|@").Split(creds, -1)
	if len(data) >= 5 {
		conf.Login.User = data[0]
		conf.Login.Pass = data[1]
		conf.Login.Host = data[2]
		conf.Login.Port = data[3]
		conf.Login.Proj = data[4]

		refresh_token(home)
	}
}

func refresh_token(home string) {
	if home == "" {
		var found bool
		home, found = os.LookupEnv("HOME")
		if !found {
			panic("No HOME dir set")
		}
	}

	conf.Login.Token = faas_login()

	err := swy.WriteYamlConfig(home + "/.swifty.conf", &conf)
	if err != nil {
		panic("Can't write swifty.conf")
	}
}

const (
	CMD_LOGIN string	= "login"
	CMD_PS string		= "ps"
	CMD_LS string		= "ls"
	CMD_INF string		= "inf"
	CMD_ADD string		= "add"
	CMD_RUN string		= "run"
	CMD_UPD string		= "upd"
	CMD_DEL string		= "del"
	CMD_LOGS string		= "logs"
	CMD_MLS string		= "mls"
	CMD_MADD string		= "madd"
	CMD_MDEL string		= "mdel"
	CMD_MENV string		= "menv"
	CMD_LUSR string		= "uls"
	CMD_UADD string		= "uadd"
	CMD_UDEL string		= "udel"
	CMD_PASS string		= "pass"
	CMD_UINF string		= "uinf"
)

var cmdOrder = []string {
	CMD_LOGIN,
	CMD_PS,
	CMD_LS,
	CMD_INF,
	CMD_ADD,
	CMD_RUN,
	CMD_UPD,
	CMD_DEL,
	CMD_LOGS,
	CMD_MLS,
	CMD_MADD,
	CMD_MDEL,
	CMD_MENV,
	CMD_LUSR,
	CMD_UADD,
	CMD_UDEL,
	CMD_PASS,
	CMD_UINF,
}

var cmdMap = map[string]*flag.FlagSet {
	CMD_LOGIN:	flag.NewFlagSet(CMD_LOGIN, flag.ExitOnError),
	CMD_PS:		flag.NewFlagSet(CMD_PS, flag.ExitOnError),
	CMD_LS:		flag.NewFlagSet(CMD_LS, flag.ExitOnError),
	CMD_INF:	flag.NewFlagSet(CMD_INF, flag.ExitOnError),
	CMD_ADD:	flag.NewFlagSet(CMD_ADD, flag.ExitOnError),
	CMD_RUN:	flag.NewFlagSet(CMD_RUN, flag.ExitOnError),
	CMD_UPD:	flag.NewFlagSet(CMD_UPD, flag.ExitOnError),
	CMD_DEL:	flag.NewFlagSet(CMD_DEL, flag.ExitOnError),
	CMD_LOGS:	flag.NewFlagSet(CMD_LOGS, flag.ExitOnError),
	CMD_MLS:	flag.NewFlagSet(CMD_MLS, flag.ExitOnError),
	CMD_MADD:	flag.NewFlagSet(CMD_MADD, flag.ExitOnError),
	CMD_MDEL:	flag.NewFlagSet(CMD_MDEL, flag.ExitOnError),
	CMD_MENV:	flag.NewFlagSet(CMD_MENV, flag.ExitOnError),
	CMD_LUSR:	flag.NewFlagSet(CMD_LUSR, flag.ExitOnError),
	CMD_UADD:	flag.NewFlagSet(CMD_UADD, flag.ExitOnError),
	CMD_UDEL:	flag.NewFlagSet(CMD_UDEL, flag.ExitOnError),
	CMD_PASS:	flag.NewFlagSet(CMD_PASS, flag.ExitOnError),
	CMD_UINF:	flag.NewFlagSet(CMD_UINF, flag.ExitOnError),
}

func bindCmdUsage(cmd, args, help string) {
	cmdMap[cmd].Usage = func() {
		fmt.Fprintf(os.Stderr, "%s %s\n  %s\n", cmd, args, help)
		cmdMap[cmd].PrintDefaults()
	}
}

func main() {
	var lang, src, mware, event, uid, name, pass string

	bindCmdUsage(CMD_LOGIN, "USER:PASS@HOST:PORT/PROJECT", "Login into the system")

	bindCmdUsage(CMD_PS, "", "List projects")

	bindCmdUsage(CMD_LS, "[PROJECT]", "List functions of a project")

	bindCmdUsage(CMD_INF, "[PROJECT] FUNCNAME", "Function info")

	cmdMap[CMD_ADD].StringVar(&lang, "lang", "auto", "Language")
	cmdMap[CMD_ADD].StringVar(&src, "src", ".", "Repository")
	cmdMap[CMD_ADD].StringVar(&mware, "mw", "", "Mware to use, comma-separated")
	cmdMap[CMD_ADD].StringVar(&event, "event", "", "Event this fn is to start")
	bindCmdUsage(CMD_ADD, "FUNCNAME", "Add a function")

	bindCmdUsage(CMD_RUN, "FUNCNAME [ARGS]", "Run a function")
	bindCmdUsage(CMD_UPD, "FUNCNAME", "Update a function")
	bindCmdUsage(CMD_DEL, "FUNCNAME", "Delete a function")
	bindCmdUsage(CMD_LOGS, "FUNCNAME", "Show function logs")

	bindCmdUsage(CMD_MLS, "[PROJECT]", "List middleware")

	bindCmdUsage(CMD_MADD, "TYPE:ID [TYPE:ID]", "Add middleware")
	bindCmdUsage(CMD_MDEL, "ID [ID]", "Delete middleware")
	bindCmdUsage(CMD_MENV, "ID", "Show middleware environment variables")

	bindCmdUsage(CMD_LUSR, "", "List users")

	cmdMap[CMD_UADD].StringVar(&uid, "id", "", "User ID (e-mail)")
	cmdMap[CMD_UADD].StringVar(&name, "name", "", "User name")
	cmdMap[CMD_UADD].StringVar(&pass, "pass", "", "User password")
	bindCmdUsage(CMD_UADD, "", "Add user")

	cmdMap[CMD_UDEL].StringVar(&uid, "id", "", "user ID (e-mail")
	bindCmdUsage(CMD_UDEL, "", "Del user")

	cmdMap[CMD_PASS].StringVar(&uid, "id", "", "User ID (e-mail)")
	cmdMap[CMD_PASS].StringVar(&pass, "pass", "", "New password")
	bindCmdUsage(CMD_PASS, "", "Set password")

	cmdMap[CMD_UINF].StringVar(&uid, "id", "", "User ID (e-mail")
	bindCmdUsage(CMD_UINF, "", "Get user info")

	flag.Usage = func() {
		for _, v := range cmdOrder {
			cmdMap[v].Usage()
		}
	}

	if len(os.Args) < 2 {
		goto usage
	}

	if val, ok := cmdMap[os.Args[1]]; ok {
		if os.Args[1] == CMD_ADD {
			val.Parse(os.Args[3:])
		} else {
			val.Parse(os.Args[2:])
		}
	} else {
		goto usage
	}

	if cmdMap[CMD_LOGIN].Parsed() {
		make_login(os.Args[2])
		return
	}

	login()

	if cmdMap[CMD_PS].Parsed() {
		list_projects()
		return
	}

	if cmdMap[CMD_LS].Parsed() {
		proj := conf.Login.Proj
		if len(os.Args) > 2 {
			proj = os.Args[2]
		}
		list_functions(proj)
		return
	}

	if cmdMap[CMD_INF].Parsed() {
		var proj, fnam string

		if len(os.Args) > 3 {
			proj = os.Args[2]
			fnam = os.Args[3]
		} else if len(os.Args) > 2 {
			proj = conf.Login.Proj
			fnam = os.Args[2]
		} else {
			goto usage
		}

		info_function(proj, fnam)
		return
	}

	if cmdMap[CMD_ADD].Parsed() {
		add_function(os.Args[2], lang, src, mware, event)
		return
	}

	if cmdMap[CMD_RUN].Parsed() {
		run_function(os.Args[2], os.Args[3:])
		return
	}

	if cmdMap[CMD_UPD].Parsed() {
		update_function(os.Args[2])
		return
	}

	if cmdMap[CMD_DEL].Parsed() {
		del_function(os.Args[2])
		return
	}

	if cmdMap[CMD_LOGS].Parsed() {
		show_logs(os.Args[2])
		return
	}

	if cmdMap[CMD_MLS].Parsed() {
		proj := conf.Login.Proj
		if len(os.Args) > 2 {
			proj = os.Args[2]
		}
		list_mware(proj)
		return
	}

	if cmdMap[CMD_MADD].Parsed() {
		add_mware(os.Args[2:])
		return
	}

	if cmdMap[CMD_MDEL].Parsed() {
		del_mware(os.Args[2:])
		return
	}

	if cmdMap[CMD_MENV].Parsed() {
		show_mware_env(os.Args[2])
		return
	}

	if cmdMap[CMD_LUSR].Parsed() {
		list_users()
		return
	}

	if cmdMap[CMD_UADD].Parsed() {
		add_user(uid, name, pass)
		return
	}

	if cmdMap[CMD_UDEL].Parsed() {
		del_user(uid)
		return
	}

	if cmdMap[CMD_PASS].Parsed() {
		set_password(uid, pass)
		return
	}

	if cmdMap[CMD_UINF].Parsed() {
		show_user_info(uid)
		return
	}

	return
usage:
	flag.Usage()
	os.Exit(1)
}
