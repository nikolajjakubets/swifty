package main

import (
	"context"
	"gopkg.in/mgo.v2/bson"
)

/* FIXME -- set up public IP address/port for this FN */

func urlEventStart(ctx context.Context, ed *FnEventDesc) error {
	err := dbFuncUpdate(ctx, bson.M{"cookie": ed.FnId, "url": false},
		bson.M{"$set": bson.M{"url": true}})
	if err == nil {
		fdm := memdGetCond(ed.FnId)
		if fdm != nil {
			fdm.public = true
		}
	}
	return err
}

func urlEventStop(ctx context.Context, ed *FnEventDesc) error {
	err := dbFuncUpdate(ctx, bson.M{"cookie": ed.FnId},
		bson.M{"$set": bson.M{"url": false}})
	if err == nil {
		fdm := memdGetCond(ed.FnId)
		if fdm != nil {
			fdm.public = false
		}
	}
	return err
}
