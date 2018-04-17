package main

import (
	"fmt"
	"context"
	"gopkg.in/robfig/cron.v2"
)

var cronRunner *cron.Cron

type EventOps struct {
	Setup func(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error
	Devel bool
}

var evtHandlers = map[string]*EventOps {
	"url":		&EventURL,
	"cron":		&EventCron,
	"mware":	&EventMware,
	"oneshot":	&EventOneShot,
}

func eventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if fn.Event.Source == "" {
		return nil
	}

	eh, ok := evtHandlers[fn.Event.Source]
	if ok && (SwyModeDevel || !eh.Devel) {
		return eh.Setup(ctx, conf, fn, on)
	} else {
		return fmt.Errorf("Unknown event type %s", fn.Event.Source)
	}
}

func oneshotEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	fn.OneShot = true
	return nil
}

var EventOneShot = EventOps {
	Setup: oneshotEventSetup,
	Devel: true,
}

func cronEventSetup(ctx context.Context, conf *YAMLConf, fn *FunctionDesc, on bool) error {
	if on {
		var fnid SwoId

		fnid = fn.SwoId
		id, err := cronRunner.AddFunc(fn.Event.CronTab, func() {
				glog.Debugf("Will run %s function, %s", fnid.Str())
			})
		if err != nil {
			ctxlog(ctx).Errorf("Can't setup cron trigger for %s", fn.SwoId.Str())
			return err
		}

		fn.CronID = int(id)
	} else {
		cronRunner.Remove(cron.EntryID(fn.CronID))
	}

	return nil
}

var EventCron = EventOps {
	Setup: cronEventSetup,
	Devel: true,
}

func eventsRestart(conf *YAMLConf) error {
	fns, err := dbFuncListWithEvents()
	if err != nil {
		glog.Errorf("Can't list functions with events: %s", err.Error())
		return err
	}

	for _, fn := range fns {
		glog.Debugf("Restart event for %s", fn.SwoId.Str())
		err = eventSetup(context.Background(), conf, &fn, true)
		if err != nil {
			return err
		}
	}

	return nil
}

func eventsInit(conf *YAMLConf) error {
	cronRunner = cron.New()
	if cronRunner == nil {
		return fmt.Errorf("can't start cron runner")
	}

	cronRunner.Start()

	return eventsRestart(conf)
}
