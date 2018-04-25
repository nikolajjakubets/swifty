package main

import (
	"strings"
	"context"
	"gopkg.in/robfig/cron.v2"
	"gopkg.in/mgo.v2/bson"
	"../common"
	"../apis/apps"
)

type FnEventCron struct {
	Tab		string			`bson:"tab"`
	Args		map[string]string	`bson:"args"`
	JobID		int			`bson:"eid"`
}

type FnEventS3 struct {
	Ns		string		`bson:"ns"`
	Bucket		string		`bson:"bucket"`
	Ops		string		`bson:"ops"`
}

func (s3 *FnEventS3)hasOp(op string) bool {
	ops := strings.Split(s3.Ops, ",")
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

type FnEventDesc struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	Name		string		`bson:"name"`
	Source		string		`bson:"source"`
	Cron		*FnEventCron	`bson:"cron,omitempty"`
	S3		*FnEventS3	`bson:"s3,omitempty"`
}

var cronRunner *cron.Cron

func cronEventStart(ctx context.Context, evt *FnEventDesc) error {
	id, err := cronRunner.AddFunc(evt.Cron.Tab, func() {
		fn, err := dbFuncFindByCookie(evt.FnId)
		if err != nil || fn == nil {
			glog.Errorf("Can't find FN %s to run Cron event", evt.FnId)
			return
		}

		if fn.State != swy.DBFuncStateRdy {
			return
		}

		doRun(context.Background(), fn, "cron", evt.Cron.Args)
	})

	if err == nil {
		evt.Cron.JobID = int(id)
	}

	return err
}

func cronEventStop(ctx context.Context, evt *FnEventDesc) error {
	cronRunner.Remove(cron.EntryID(evt.Cron.JobID))
	return nil
}

func eventsInit(conf *YAMLConf) error {
	cronRunner = cron.New()
	cronRunner.Start()
	return nil
}

func (e *FnEventDesc)toAPI(withid bool) *swyapi.FunctionEvent {
	ae := swyapi.FunctionEvent{
		Name: e.Name,
		Source: e.Source,
	}

	if withid {
		ae.Id = e.ObjID.Hex()
	}

	if e.Cron != nil {
		ae.Cron = &swyapi.FunctionEventCron {
			Tab: e.Cron.Tab,
			Args: e.Cron.Args,
		}
	}

	if e.S3 != nil {
		ae.S3 = &swyapi.FunctionEventS3 {
			Bucket: e.S3.Bucket,
			Ops: e.S3.Ops,
		}
	}

	return &ae
}

func eventsList(fnid string) ([]swyapi.FunctionEvent, *swyapi.GateErr) {
	var ret []swyapi.FunctionEvent
	evs, err := dbListFnEvents(fnid)
	if err != nil {
		return ret, GateErrD(err)
	}

	for _, e := range evs {
		ret = append(ret, *e.toAPI(true))
	}
	return ret, nil
}

func eventsAdd(ctx context.Context, fnid string, evt *swyapi.FunctionEvent) (string, *swyapi.GateErr) {
	ed := &FnEventDesc{
		ObjID: bson.NewObjectId(),
		Name: evt.Name,
		FnId: fnid,
		Source: evt.Source,
	}

	var err error

	switch evt.Source {
	case "cron":
		ed.Cron = &FnEventCron{
			Tab: evt.Cron.Tab,
			Args: evt.Cron.Args,
		}

		err = cronEventStart(ctx, ed)
	case "s3":
		ed.S3 = &FnEventS3{
			Bucket: evt.S3.Bucket,
			Ops: evt.S3.Ops,
		}
		err = s3EventStart(ctx, ed)
	case "url":
		err = urlEventStart(ctx, ed)
	default:
		return "", GateErrM(swy.GateBadRequest, "Unsupported event type")
	}

	if err != nil {
		return "", GateErrM(swy.GateGenErr, "Can't setup event")
	}

	err = dbAddEvent(ed)
	if err != nil {
		eventStop(ctx, ed)
		return "", GateErrD(err)
	}

	return ed.ObjID.Hex(), nil
}

func eventsGet(fnid, eid string) (*swyapi.FunctionEvent, *swyapi.GateErr) {
	ed, err := dbFindEvent(eid)
	if err != nil {
		return nil, GateErrD(err)
	}

	if ed.FnId != fnid {
		return nil, GateErrC(swy.GateNotFound)
	}

	return ed.toAPI(false), nil
}

func eventStop(ctx context.Context, ed *FnEventDesc) error {
	var err error

	switch ed.Source {
	case "cron":
		err = cronEventStop(ctx, ed)
	case "s3":
		err = s3EventStop(ctx, ed)
	case "url":
		err = urlEventStop(ctx, ed)
	}

	return err
}

func eventsDelete(ctx context.Context, fnid, eid string) *swyapi.GateErr {
	ed, err := dbFindEvent(eid)
	if err != nil {
		return GateErrD(err)
	}

	if ed.FnId != fnid {
		return GateErrC(swy.GateNotFound)
	}

	err = eventStop(ctx, ed)
	if err != nil {
		return GateErrM(swy.GateGenErr, "Can't stop event")
	}

	err = dbRemoveEvent(ed)
	if err != nil {
		return GateErrD(err)
	}

	return nil
}

func clearAllEvents(ctx context.Context, fn *FunctionDesc) error {
	evs, err := dbListFnEvents(fn.Cookie)
	if err != nil {
		return err
	}

	for _, e := range evs {
		err = eventStop(ctx, &e)
		if err != nil {
			return err
		}

		err = dbRemoveEvent(&e)
		if err != nil {
			return err
		}
	}

	return nil
}
