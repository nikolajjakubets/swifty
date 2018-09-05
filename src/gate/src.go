package main

import (
	"gopkg.in/yaml.v2"
	"gopkg.in/mgo.v2/bson"
	"fmt"
	"bytes"
	"strings"
	"os/exec"
	"os"
	"context"
	"io"
	"time"
	"io/ioutil"
	"encoding/base64"
	"strconv"
	"errors"
	"../common"
	"../common/xwait"
	"../common/http"
	"../apis"
)

const (
	RepoDescFile	= ".swifty.yml"
)

func fnCodeDir(fn *FunctionDesc) string {
	return fn.Tennant + "/" + fn.Project + "/" + fn.Name
}

func fnCodeVersionDir(fn *FunctionDesc, version string) string {
	return fnCodeDir(fn) + "/" + version
}

func fnCodeLatestDir(fn *FunctionDesc) string {
	return fnCodeVersionDir(fn, fn.Src.Version)
}

func fnCodeVersionPath(conf *YAMLConf, fn *FunctionDesc, version string) string {
	return conf.Wdog.Volume + "/" + fnCodeVersionDir(fn, version)
}

func fnCodeLatestPath(conf *YAMLConf, fn *FunctionDesc) string {
	return fnCodeVersionPath(conf, fn, fn.Src.Version)
}

func cloneDir() string {
	return conf.Home + "/" + CloneDir
}

var repStates = map[int]string {
	swy.DBRepoStateCln:	"cloning",
	swy.DBRepoStateStl:	"stalled",
	swy.DBRepoStateRem:	"removing",
	swy.DBRepoStateRdy:	"ready",
}

type RepoDesc struct {
	// These objects are kept in Mongo, which requires the below
	// field to be present...
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	SwoId				`bson:",inline"`
	Type		string		`bson:"type"`
	State		int		`bson:"state"`
	Commit		string		`bson:"commit,omitempty"`
	UserData	string		`bson:"userdata,omitempty"`
	Pull		string		`bson:"pulling"`

	Path		string		`bson:"path"`
	LastPull	*time.Time	`bson:"last_pull,omitempty"`

	AccID		bson.ObjectId	`bson:"accid,omitempty"`
}

type GitHubRepo struct {
	Name		string		`json:"name"`
	URL		string		`json:"clone_url"`
	Private		bool		`json:"private"`
}

type GitHubUser struct {
	Login		string		`json:"login"`
}


func (rd *RepoDesc)path() string {
	if rd.Path != "" {
		return rd.Path
	}

	return rd.SwoId.Tennant + "/" + rd.ObjID.Hex()
}

func (rd *RepoDesc)clonePath() string {
	return cloneDir() + "/" + rd.path()
}

func (rd *RepoDesc)URL() string { return rd.SwoId.Name }

func getRepoDesc(id *SwoId, params *swyapi.RepoAdd) *RepoDesc {
	rd := &RepoDesc {
		SwoId:		*id,
		Type:		params.Type,
		UserData:	params.UserData,
		Pull:		params.Pull,
	}

	return rd
}

func (rd *RepoDesc)toInfo(ctx context.Context, details bool) (*swyapi.RepoInfo, *swyapi.GateErr) {
	r := &swyapi.RepoInfo {
		ID:		rd.ObjID.Hex(),
		Type:		rd.Type,
		URL:		rd.URL(),
		State:		repStates[rd.State],
		Commit:		rd.Commit,
		AccID:		rd.AccID.Hex(),
	}

	if details {
		r.UserData = rd.UserData
		r.Pull = rd.Pull

		dfile := rd.clonePath() + "/" + RepoDescFile
		if _, err := os.Stat(dfile); err == nil {
			r.Desc = true
		} else {
			r.Desc = false
		}
	}

	return r, nil
}

type repoHandler struct {
	clone func (context.Context, *RepoDesc, *AccDesc) (string, error)
}

var repoHandlers = map[string]repoHandler {
	"github":	{
		clone:	cloneGit,
	},
	"gitlab":	{
		clone:	cloneGit,
	},
}

func (rd *RepoDesc)Attach(ctx context.Context, ac *AccDesc) *swyapi.GateErr {
	rd.ObjID = bson.NewObjectId()
	rd.State = swy.DBRepoStateCln
	if ac != nil {
		rd.AccID = ac.ObjID
	}

	rh, ok := repoHandlers[rd.Type]
	if !ok {
		return GateErrM(swy.GateBadRequest, "Unsupported repo type")
	}

	err := dbInsert(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	go bgClone(rd, ac, &rh)

	return nil
}

func (rd *RepoDesc)Update(ctx context.Context, ru *swyapi.RepoUpdate) *swyapi.GateErr {
	if ru.Pull != nil {
		rd.Pull = *ru.Pull
		err := dbUpdatePart(ctx, rd, bson.M{"pulling": rd.Pull})
		if err != nil {
			return GateErrD(err)
		}
	}

	return nil
}

func (rd *RepoDesc)Detach(ctx context.Context, conf *YAMLConf) *swyapi.GateErr {
	err := dbUpdatePart(ctx, rd, bson.M{"state": swy.DBRepoStateRem})
	if err != nil {
		return GateErrD(err)
	}

	rd.State = swy.DBRepoStateRem

	if rd.Path == "" {
		_, err = swy.DropDir(cloneDir(), rd.path())
		if err != nil {
			return GateErrE(swy.GateFsError, err)
		}
	}

	err = dbRemove(ctx, rd)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func (rd *RepoDesc)description(ctx context.Context) (*swyapi.RepoDesc, *swyapi.GateErr) {
	dfile := rd.clonePath() + "/" + RepoDescFile
	if _, err := os.Stat(dfile); os.IsNotExist(err) {
		return nil, GateErrM(swy.GateNotAvail, "No description for repo")
	}

	var out swyapi.RepoDesc

	desc, err := ioutil.ReadFile(dfile)
	if err != nil {
		return nil, GateErrE(swy.GateFsError, err)
	}

	err = yaml.Unmarshal(desc, &out)
	if err != nil {
		return nil, GateErrE(swy.GateGenErr, err)
	}

	return &out, nil
}

func (rd *RepoDesc)readFile(ctx context.Context, fname string) ([]byte, *swyapi.GateErr) {
	dfile := rd.clonePath() + "/" + fname
	if _, err := os.Stat(dfile); os.IsNotExist(err) {
		return nil, GateErrM(swy.GateNotAvail, "No such file in repo")
	}

	cont, err := ioutil.ReadFile(dfile)
	if err != nil {
		return nil, GateErrM(swy.GateFsError, "Error reading file")
	}

	return cont, nil
}

func (rd *RepoDesc)listFiles(ctx context.Context) ([]*swyapi.RepoFile, *swyapi.GateErr) {
	rp := rd.clonePath()
	root := swyapi.RepoFile { Path: "", Children: &[]*swyapi.RepoFile{} }
	dirs := []*swyapi.RepoFile{&root}

	for len(dirs) > 0 {
		dir := dirs[0]
		dirs = dirs[1:]

		ents, err := ioutil.ReadDir(rp + "/" + dir.Path)
		if err != nil {
			return nil, GateErrM(swy.GateFsError, "Cannot list files in repo")
		}

		for _, ent := range ents {
			e := &swyapi.RepoFile{
				Label:	ent.Name(),
			}
			if dir.Path == "" {
				e.Path = ent.Name()
			} else {
				e.Path = dir.Path + "/" + ent.Name()
			}

			if ent.IsDir() {
				if e.Label == ".git" {
					continue
				}

				e.Type = "dir"
				e.Children = &[]*swyapi.RepoFile{}
				dirs = append(dirs, e)
			} else {
				e.Type = "file"
				lng := RtLangDetect(e.Label)
				e.Lang = &lng
			}

			l := *dir.Children
			l = append(l, e)
			dir.Children = &l
		}
	}

	return *root.Children, nil
}

func (rd *RepoDesc)pull(ctx context.Context) *swyapi.GateErr {
	if rd.LastPull != nil && time.Now().Before( rd.LastPull.Add(time.Duration(conf.RepoSyncRate) * time.Minute)) {
		return GateErrM(swy.GateNotAvail, "To frequent sync")
	}

	go func() {
		pctx, done := mkContext("::repo-pull")
		rd.pullSync(pctx)
		done(pctx)
	}()

	return nil
}

func (rd *RepoDesc)pullSync(ctx context.Context) *swyapi.GateErr {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()
	ctxlog(ctx).Debugf("Git pull %s", clone_to)

	cmd := exec.Command("git", "-C", clone_to, "pull")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't pull %s -> %s: %s (%s:%s)",
			rd.URL(), clone_to, err.Error(),
			stdout.String(), stderr.String())
		return GateErrE(swy.GateGenErr, err)
	}

	cmt, err := gitCommit(clone_to)
	if err == nil {
		t := time.Now()
		dbUpdatePart(ctx, rd, bson.M{"commit": cmt, "last_pull": &t})
	}

	return nil
}

func pullRepos(ts time.Time) error {
	ctx, done := mkContext("::reposync")
	defer done(ctx)

	var rds []*RepoDesc

	err := dbFindAll(ctx, bson.M{
			"pulling": "periodic",
			"last_pull": bson.M{"$lt": ts},
		}, &rds)
	if err != nil {
		if !dbNF(err) {
			ctxlog(ctx).Debugf("Can't get repos to sync: %s", err.Error())
		}

		return err
	}

	synced := 0

	for _, rd := range rds {
		if rd.pullSync(ctx) == nil {
			synced++
		}
	}

	ctxlog(ctx).Debugf("Synced %d repos (%d not)", synced, len(rds) - synced)

	return nil
}

func periodicPullRepos(period time.Duration) {
	for {
		t := time.Now()
		nxt := period

		if pullRepos(t.Add(-period)) != nil {
			nxt = 5 * time.Minute /* Will try in 5 minutes */
		}

		t = t.Add(nxt)
		glog.Debugf("Next repo sync at %s", t.String())
		<-time.After(t.Sub(time.Now()))
	}
}

var demoRep RepoDesc

func ReposInit(ctx context.Context, conf *YAMLConf) error {
	go periodicPullRepos(time.Duration(conf.RepoSyncPeriod) * time.Minute)

	ctxlog(ctx).Debugf("Resolve %s repo", conf.DemoRepo.URL)
	q := bson.M{
		"type": "github",
		"name": conf.DemoRepo.URL,
		"tennant": "*",
		"project": NoProject,
		"state": swy.DBRepoStateRdy,
	}
	err := dbFind(ctx, q, &demoRep)
	if err != nil && ! dbNF(err) {
		return err
	}

	ctxlog(ctx).Debugf("Resolved remo repo: %s", demoRep.ObjID.Hex())
	return nil
}

func gitCommit(dir string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command("git", "-C", dir, "log", "-n1", "--pretty=format:%H")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return stdout.String(), nil
}

var srcHandlers = map[string] struct {
	get func (context.Context, *swyapi.FunctionSources, string, string) error
} {
	"git":		{ get: getFileFromRepo, },
	"code":		{ get: getFileFromReq, },
}

func checkVersion(ctx context.Context, fn *FunctionDesc, version string, versions []string) (bool, error) {
	cver, _ := strconv.Atoi(version)
	for _, v := range versions {
		/* For files we just generate sequential numbers */
		hver, _ := strconv.Atoi(v)
		if cver <= hver {
			return true, nil
		}
	}

	return false, nil
}

func getSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	fn.Src.Version = zeroVersion
	return writeSources(ctx, fn, src)
}

func updateSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	ov, _ := strconv.Atoi(fn.Src.Version)
	fn.Src.Version = strconv.Itoa(ov + 1)
	return writeSources(ctx, fn, src)
}

func writeSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) error {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return fmt.Errorf("Unknown sources type %s", src.Type)
	}

	return srch.get(ctx, src, fnCodeLatestPath(&conf, fn), RtScriptName(&fn.Code, ""))
}

func writeTempSources(ctx context.Context, fn *FunctionDesc, src *swyapi.FunctionSources) (string, error) {
	srch, ok := srcHandlers[src.Type]
	if !ok {
		return "", fmt.Errorf("Unknown sources type %s", src.Type)
	}

	suff := "tmp" /* FIXME -- locking or randomness */
	return suff, srch.get(ctx, src, fnCodeLatestPath(&conf, fn), RtScriptName(&fn.Code, suff))
}

func bgClone(rd *RepoDesc, ac *AccDesc, rh *repoHandler) {
	ctx, done := mkContext("::gitclone")
	defer done(ctx)

	commit, err := rh.clone(ctx, rd, ac)
	if err != nil {
		/* FIXME -- keep logs and show them user */
		dbUpdatePart(ctx, rd, bson.M{ "state": swy.DBRepoStateStl })
		return
	}

	t := time.Now()
	dbUpdatePart(ctx, rd, bson.M{
					"state": swy.DBRepoStateRdy,
					"commit": commit,
					"last_pull": &t,
				})
}

func cloneGit(ctx context.Context, rd *RepoDesc, ac *AccDesc) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	clone_to := rd.clonePath()

	_, err := os.Stat(clone_to)
	if err == nil || !os.IsNotExist(err) {
		ctxlog(ctx).Errorf("repo for %s is already there", rd.SwoId.Str())
		return "", fmt.Errorf("can't clone repo")
	}

	if os.MkdirAll(clone_to, 0777) != nil {
		ctxlog(ctx).Errorf("can't create %s: %s", clone_to, err.Error())
		return "", err
	}

	curl := rd.URL()

	if ac != nil {
		if ac.Type != "github" {
			return "", errors.New("Corrupted acc type")
		}

		t, err := ac.Secrets["token"].value()
		if err != nil {
			return "", err
		}

		if t != "" && strings.HasPrefix(curl, "https://") {
			curl = "https://" + ac.SwoId.Name + ":" + t + "@" + curl[8:]
		}
	}

	ctxlog(ctx).Debugf("Git clone %s -> %s", curl, clone_to)

	cmd := exec.Command("git", "-C", clone_to, "clone", "--depth=1", curl, ".")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		ctxlog(ctx).Errorf("can't clone %s -> %s: %s (%s:%s)",
				rd.URL(), clone_to, err.Error(),
				stdout.String(), stderr.String())
		return "", err
	}

	return gitCommit(clone_to)
}

func writeSourceFile(ctx context.Context, to, script string, data []byte) error {
	err := os.MkdirAll(to, 0750)
	if err != nil {
		ctxlog(ctx).Error("Can't mkdir sources: %s", err.Error())
		return errors.New("FS error")
	}

	err = ioutil.WriteFile(to + "/" + script, data, 0600)
	if err != nil {
		ctxlog(ctx).Error("Can't write sources: %s", err.Error())
		return errors.New("FS error")
	}

	return nil
}

func ctxRepoId(ctx context.Context, rid string) bson.M {
	return  bson.M{
		"tennant": bson.M { "$in": []string{gctx(ctx).Tenant, "*"}},
		"_id": bson.ObjectIdHex(rid),
	}
}

func getFileFromRepo(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	ids := strings.SplitN(src.Repo, "/", 2)
	if len(ids) != 2 || !bson.IsObjectIdHex(ids[0]) {
		return errors.New("Bad repo file ID")
	}

	var rd RepoDesc
	err := dbFind(ctx, ctxRepoId(ctx, ids[0]), &rd)
	if err != nil {
		return err
	}

	fnCode, err := ioutil.ReadFile(rd.clonePath() + "/" + ids[1])
	if err != nil {
		return err
	}

	return writeSourceFile(ctx, to, script, fnCode)
}

func getFileFromReq(ctx context.Context, src *swyapi.FunctionSources, to, script string) error {
	data, err := base64.StdEncoding.DecodeString(src.Code)
	if err != nil {
		return fmt.Errorf("Error decoding sources")
	}

	return writeSourceFile(ctx, to, script, data)
}

func GCOldSources(ctx context.Context, fn *FunctionDesc, ver string) {
	np, err := swy.DropDirPrep(conf.Wdog.Volume, fnCodeVersionDir(fn, ver))
	if err != nil {
		ctxlog(ctx).Errorf("Leaking %s sources till FN removal (err %s)", ver, err.Error())
		return
	}

	if np == "" {
		return
	}

	w := xwait.Prepare(fn.Cookie)
	cookie := fn.Cookie
	ctxlog(ctx).Debugf("Will remove %s's sources after a while via %s", ver, np)

	go func() {
		tmo := 16 * 60 * time.Second
		ctx, done := mkContext("::gcoldsource")
		defer done(ctx)

		for {
			vers, err := dbBalancerListVersions(ctx, cookie)
			if err != nil {
				break /* What to do? */
			}

			found := false
			for _, v := range(vers) {
				if ver == v {
					found = true
					break
				}
			}

			if !found {
				ctxlog(ctx).Debugf("Dropping %s.%s sources", cookie, ver)
				swy.DropDirComplete(np)
				break
			}

			if w.Wait(&tmo) {
				ctxlog(ctx).Errorf("Leaking %s sources till FN removal (tmo)", ver)
				break
			}
		}

		w.Done()
	}()
}

func listReposGH(ac *AccDesc) ([]*GitHubRepo, error) {
	var rq *swyhttp.RestReq

	t, err := ac.Secrets["token"].value()
	if err != nil {
		return nil, err
	}

	if t == "" {
		rq = &swyhttp.RestReq{
			Address: "https://api.github.com/users/" + ac.SwoId.Name + "/repos",
			Method: "GET",
		}
	} else {
		rq = &swyhttp.RestReq{
			Address: "https://api.github.com/user/repos?access_token=" + t,
			Method: "GET",
		}
	}

	rsp, err := swyhttp.MarshalAndPost(rq, nil)
	if err != nil {
		return nil, err
	}

	var grs []*GitHubRepo
	err = swyhttp.ReadAndUnmarshalResp(rsp, &grs)
	if err != nil {
		return nil, err
	}

	return grs, nil
}

func listRepos(ctx context.Context, accid, att string) ([]*swyapi.RepoInfo, *swyapi.GateErr) {
	ret := []*swyapi.RepoInfo{}
	urls := make(map[string]bool)

	if att == "" || att == "true" {
		var reps []*RepoDesc

		q := bson.D{
			{"tennant", bson.M {
				"$in": []string{gctx(ctx).Tenant, "*" },
			}},
			{"project", NoProject},
		}
		err := dbFindAll(ctx, q, &reps)
		if err != nil {
			return nil, GateErrD(err)
		}

		for _, rp := range reps {
			if accid != "" && accid != rp.AccID.Hex() {
				continue
			}

			ri, cerr := rp.toInfo(ctx, false)
			if cerr != nil {
				return nil, cerr
			}

			ret = append(ret, ri)
			urls[ri.URL] = true
		}
	}

	if att == "" || att == "false" {
		/* FIXME -- maybe cache repos in a DB? */
		var acs []*AccDesc

		q := bson.M{"type": "github", "tennant":  gctx(ctx).Tenant}
		if accid != "" {
			q["_id"] = bson.ObjectIdHex(accid)
		}
		err := dbFindAll(ctx, q, &acs)
		if err != nil && !dbNF(err) {
			return nil, GateErrD(err)
		}

		for _, ac := range acs {
			grs, err := listReposGH(ac)
			if err != nil {
				ctxlog(ctx).Errorf("Can't list GH repos: %s", err.Error())
				continue
			}

			for _, gr := range grs {
				if _, ok := urls[gr.URL]; ok {
					continue
				}

				ri := &swyapi.RepoInfo {
					Type:	"github",
					URL:	gr.URL,
					State:	"unattached",
				}

				if gr.Private {
					ri.AccID = ac.ObjID.Hex()
				}

				ret = append(ret, ri)
				urls[gr.URL] = true
			}
		}
	}

	return ret, nil
}

func cleanRepo(ctx context.Context, fn *FunctionDesc) error {
	sd := fnCodeDir(fn)

	td, err := swy.DropDir(conf.Home + "/" + CloneDir, sd)
	if err != nil {
		return err
	}

	if td != "" {
		ctxlog(ctx).Debugf("Will remove %s repo clone via %s", fn.SwoId.Str(), td)
	}

	td, err = swy.DropDir(conf.Wdog.Volume, sd)
	if err != nil {
		return err
	}

	if td != "" {
		ctxlog(ctx).Debugf("Will remove %s sources via %s", fn.SwoId.Str(), td)
	}

	return nil
}

func update_deps(ctx context.Context, repo_path string) error {
	// First -- check git submodules
	_, err := os.Stat(repo_path + "/.gitmodules")
	if err == nil {
		err = update_git_submodules(ctx, repo_path)
	} else if !os.IsNotExist(err) {
		err = fmt.Errorf("Can't update git submodules: %s", err.Error())
	} else {
		err = nil
	}

	if err != nil {
		ctxlog(ctx).Error("Can't update git submodules")
		return err
	}

	return nil
}

func update_git_submodules(ctx context.Context, repo_path string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	ctxlog(ctx).Debugf("Updating git submodules @%s", repo_path)

	cmd := exec.Command("git", "-C", repo_path, "submodule", "init")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return err
	}

	cmd = exec.Command("git", "-C", repo_path, "submodule", "update")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

// Checkout helpers -- this code just copies the tree around skipping
// the .git ones everywhere.
func copy_git_files(from, to string) error {
	st, err := os.Stat(from)
	if err != nil {
		return co_err(from, "stat", err)
	}

	err = os.MkdirAll(to, st.Mode())
	if err != nil {
		return co_err(to, "mkdirall", err)
	}

	return copy_dir(from, to)
}

func copy_dir(from, to string) error {
	dir, err := os.Open(from)
	if err != nil {
		return co_err(from, "opendir", err)
	}

	ents, err := dir.Readdir(-1)
	dir.Close() // This keeps minimum fds across recursion below
	if err != nil {
		return co_err(from, "readdir", err)
	}

	for _, ent := range ents {
		ff := from + "/" + ent.Name()
		ft := to + "/" + ent.Name()

		if ent.IsDir() {
			if ent.Name() == ".git" {
				continue
			}
			err = os.Mkdir(ft, ent.Mode())
			if err != nil {
				return co_err(ft, "mkdir", err)
			}

			err = copy_dir(ff, ft)
		} else {
			mode := ent.Mode()
			if mode & os.ModeType == 0 {
				err = copy_file(ff, ft, mode)
			} else {
				err = copy_node(ff, ft, mode)
			}
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func copy_file(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return co_err(from, "open", err)
	}
	defer in.Close()

	out, err := os.Create(to)
	if err != nil {
		return co_err(to, "create", err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return co_err(to, "copy", err)
	}

	err = os.Chmod(to, mode)
	if err != nil {
		return co_err(to, "chmod", err)
	}

	return nil
}

func copy_node(from, to string, mode os.FileMode) error {
	if mode & os.ModeSymlink != 0 {
		tgt, err := os.Readlink(from)
		if err != nil {
			return co_err(from, "readlink", err)
		}

		err = os.Symlink(tgt, to)
		if err != nil {
			return co_err(to, "symlink", err)
		}

		return nil
	}

	return fmt.Errorf("Unsupported mode (%s)", from)
}

func co_err(fn, reason string, o_err error) error {
	return fmt.Errorf("Error on %s (%s): %s", reason, fn, o_err.Error())
}

