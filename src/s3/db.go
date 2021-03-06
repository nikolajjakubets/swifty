/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"

	"context"
	"reflect"
	"time"
	"fmt"
	"swifty/common"
	"swifty/s3/mgo"
)

var dbColMap map[reflect.Type]string
var session *mgo.Session

const (
	S3StateNone			= 0
	S3StateActive			= 1
	S3StateInactive			= 2
)

var s3StateTransition = map[uint32][]uint32 {
	S3StateNone:		[]uint32{ S3StateNone, },
	S3StateActive:		[]uint32{ S3StateNone, },
	S3StateInactive:	[]uint32{ S3StateActive, },
}

func dbNF(err error) bool {
	return err == mgo.ErrNotFound
}

func dbConnect(conf *YAMLConf) error {
	var err error

	dbc := xh.ParseXCreds(conf.DB)
	pwd, err := s3Secrets.Get(dbc.Pass)
	if err != nil {
		log.Errorf("No DB password found: %s", err.Error())
		return err
	}

	info := mgo.DialInfo{
		Addrs:		[]string{dbc.Addr()},
		Database:	s3mgo.DBName,
		Timeout:	60 * time.Second,
		Username:	dbc.User,
		Password:	pwd,
	}

	session, err = mgo.DialWithInfo(&info);
	if err != nil {
		log.Errorf("dbConnect: Can't dial to %s with db %s (%s)",
				conf.DB, s3mgo.DBName, err.Error())
		return err
	}

	session.SetMode(mgo.Monotonic, true)

	s := session.Copy()
	defer s.Close()

	// Make sure the indices are present
	index := mgo.Index{
			Unique:		true,
			DropDups:	true,
			Background:	true,
			Sparse:		true}

	index.Key = []string{"namespace"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"user"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Iams).EnsureIndex(index)

	index.Key = []string{"bcookie"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Buckets).EnsureIndex(index)
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Websites).EnsureIndex(index)

	index.Key = []string{"uid"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"ucookie"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Uploads).EnsureIndex(index)

	index.Key = []string{"access-key-id"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3AccessKeys).EnsureIndex(index)

	index.Unique = false
	index.Key = []string{"ocookie"}
	s.DB(s3mgo.DBName).C(s3mgo.DBColS3Objects).EnsureIndex(index)

	dbColMap = make(map[reflect.Type]string)
	dbColMap[reflect.TypeOf(s3mgo.Iam{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(&s3mgo.Iam{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf([]s3mgo.Iam{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(&[]s3mgo.Iam{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(s3mgo.Account{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(&s3mgo.Account{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf([]s3mgo.Account{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(&[]s3mgo.Account{})] = s3mgo.DBColS3Iams
	dbColMap[reflect.TypeOf(&s3mgo.AcctStats{})] = s3mgo.DBColS3Stats
	dbColMap[reflect.TypeOf(s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf(&s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf([]s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf(&[]s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf([]*s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf(&[]*s3mgo.AccessKey{})] = s3mgo.DBColS3AccessKeys
	dbColMap[reflect.TypeOf(s3mgo.Bucket{})] = s3mgo.DBColS3Buckets
	dbColMap[reflect.TypeOf(&s3mgo.Bucket{})] = s3mgo.DBColS3Buckets
	dbColMap[reflect.TypeOf([]s3mgo.Bucket{})] = s3mgo.DBColS3Buckets
	dbColMap[reflect.TypeOf(&[]s3mgo.Bucket{})] = s3mgo.DBColS3Buckets
	dbColMap[reflect.TypeOf(s3mgo.Object{})] = s3mgo.DBColS3Objects
	dbColMap[reflect.TypeOf(&s3mgo.Object{})] = s3mgo.DBColS3Objects
	dbColMap[reflect.TypeOf([]s3mgo.Object{})] = s3mgo.DBColS3Objects
	dbColMap[reflect.TypeOf(&[]s3mgo.Object{})] = s3mgo.DBColS3Objects
	dbColMap[reflect.TypeOf(S3Upload{})] = s3mgo.DBColS3Uploads
	dbColMap[reflect.TypeOf(&S3Upload{})] = s3mgo.DBColS3Uploads
	dbColMap[reflect.TypeOf([]S3Upload{})] = s3mgo.DBColS3Uploads
	dbColMap[reflect.TypeOf(&[]S3Upload{})] = s3mgo.DBColS3Uploads
	dbColMap[reflect.TypeOf(s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf(&s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf([]s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf(&[]s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf([]*s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf(&[]*s3mgo.ObjectPart{})] = s3mgo.DBColS3ObjectData
	dbColMap[reflect.TypeOf(s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf(&s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf([]s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf(&[]s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf([]*s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf(&[]*s3mgo.DataChunk{})] = s3mgo.DBColS3DataChunks
	dbColMap[reflect.TypeOf(&S3Website{})] = s3mgo.DBColS3Websites

	return nil
}

func dbDisconnect() {
	session.Close()
	session = nil
}

func dbRepair(ctx context.Context) error {
	var err error

	log.Debugf("s3: Running db consistency test/repair")

	if err = s3RepairUpload(ctx); err != nil {
		return err
	}

	if err = s3RepairObject(ctx); err != nil {
		return err
	}

	if err = s3RepairObjectData(ctx); err != nil {
		return err
	}

	if err = s3RepairBucket(ctx); err != nil {
		return err
	}

	log.Debugf("s3: Finished db consistency test/repair")
	return nil
}

func dbColl(object interface{}) (string) {
	if name, ok := dbColMap[reflect.TypeOf(object)]; ok {
		return name
	}
	log.Fatalf("Unmapped object %v", object)
	return ""
}

func infoLong(o interface{}) (string) {
	switch (reflect.TypeOf(o)) {
	case reflect.TypeOf(&s3mgo.AccessKey{}):
		akey := o.(*s3mgo.AccessKey)
		return fmt.Sprintf("{ S3AccessKey: %s/%s/%s/%d }",
			akey.ObjID, akey.IamObjID,
			akey.AccessKeyID, akey.State)
	case reflect.TypeOf(&s3mgo.Account{}):
		account := o.(*s3mgo.Account)
		return fmt.Sprintf("{ S3Account: %s/%s/%d/%s/%s }",
			account.ObjID, account.Namespace,
			account.State, account.User, account.Email)
	case reflect.TypeOf(&s3mgo.Iam{}):
		iam := o.(*s3mgo.Iam)
		return fmt.Sprintf("{ S3Iam: %s/%s/%d/%s }",
			iam.ObjID, iam.AccountObjID, iam.State,
			iam.Policy.InfoLong())
	case reflect.TypeOf(&s3mgo.Bucket{}):
		bucket := o.(*s3mgo.Bucket)
		return fmt.Sprintf("{ S3Bucket: %s/%s/%s/%d/%s }",
			bucket.ObjID, bucket.BCookie,
			bucket.NamespaceID, bucket.State,
			bucket.Name)
	case reflect.TypeOf(&s3mgo.ObjectPart{}):
		objd := o.(*s3mgo.ObjectPart)
		return fmt.Sprintf("{ S3ObjectPart: %s/%s/%s/%s/%d/%d }",
			objd.ObjID, objd.RefID,
			objd.BCookie, objd.OCookie,
			objd.State, objd.Size)
	case reflect.TypeOf(&s3mgo.Object{}):
		object := o.(*s3mgo.Object)
		return fmt.Sprintf("{ S3Object: %s/%s/%s/%d/%s }",
			object.ObjID, object.BucketObjID,
			object.OCookie, object.State,
			object.Key)
	case reflect.TypeOf(&S3Upload{}):
		upload := o.(*S3Upload)
		return fmt.Sprintf("{ S3Upload: %s/%s/%s/%d/%s }",
			upload.ObjID, upload.BucketObjID,
			upload.UploadID, upload.Ref, upload.Key)
	}
	return "{ Unknown type }"
}

func dbS3SetObjID(o interface{}, query bson.M) {
	if _, ok := query["_id"]; ok == false {
		elem := reflect.ValueOf(o).Elem()
		val := elem.FieldByName("ObjID")
		if val != reflect.ValueOf(nil) {
			id := val.Interface().(bson.ObjectId)
			if id != "" {
				query["_id"] = id
			}
		}
	}
}

func dbS3UpdateMTime(query bson.M) {
	if val, ok := query["$set"]; ok {
		val.(bson.M)["mtime"] = current_timestamp()
	}
}

func dbS3SetMTime(o interface{}) {
	elem := reflect.ValueOf(o).Elem()
	val := elem.FieldByName("MTime")
	if val != reflect.ValueOf(nil) {
		val.SetInt(current_timestamp())
	}
}

func dbS3Insert(ctx context.Context, o interface{}) (error) {
	dbS3SetMTime(o)

	err := Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Insert(o)
	if err != nil {
		log.Errorf("dbS3Insert: %s: %s", infoLong(o), err.Error())
	}
	return err
}

func dbS3Update(ctx context.Context, query bson.M, update bson.M, retnew bool, o interface{}) (error) {
	if query == nil { query = make(bson.M) }

	dbS3SetObjID(o, query)
	dbS3UpdateMTime(update)

	c := Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		false,
		Update:		update,
		ReturnNew:	retnew,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3Upsert(ctx context.Context, query bson.M, update bson.M, o interface{}) (error) {
	if query == nil { query = make(bson.M) }

	c := Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		true,
		Remove:		false,
		Update:		update,
		ReturnNew:	true,
	}
	_, err := c.Find(query).Apply(change, o)
	return err
}

func dbS3SetOnState(ctx context.Context, o interface{}, state uint32, query bson.M, fields bson.M) (error) {
	if query == nil { query = make(bson.M) }

	query["state"] = bson.M{"$in": s3StateTransition[state]}
	update := bson.M{"$set": fields}

	err := dbS3Update(ctx, query, update, true, o)
	if err != nil {
		log.Errorf("s3: Can't set state %d on %s: %s",
			state, infoLong(o), err.Error())
	}
	return err
}

func dbS3SetState(ctx context.Context, o interface{}, state uint32, query bson.M) (error) {
	return dbS3SetOnState(ctx, o, state, query, bson.M{"state": state})
}

func dbS3SetState2(ctx context.Context, o interface{}, state uint32, upd bson.M) (error) {
	upd["state"] = state
	return dbS3SetOnState(ctx, o, state, nil, upd)
}

func dbS3RemoveCond(ctx context.Context, o interface{}, query bson.M) (error) {
	if query == nil { query = make(bson.M) }

	dbS3SetObjID(o, query)

	c := Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o))
	change := mgo.Change{
		Upsert:		false,
		Remove:		true,
		ReturnNew:	false,
	}
	_, err := c.Find(query).Apply(change, o)
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("dbS3RemoveCond: Can't remove %s: %s",
			infoLong(o), err.Error())
	}
	return err
}

func dbS3RemoveOnState(ctx context.Context, o interface{}, state uint32, query bson.M) (error) {
	if query == nil { query = make(bson.M) }

	query["state"] = state

	return dbS3RemoveCond(ctx, o, query)
}

func dbS3Remove(ctx context.Context, o interface{}) (error) {
	return dbS3RemoveCond(ctx, o, nil)
}

func dbS3FindOne(ctx context.Context, query bson.M, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).One(o)
}

func dbS3FindOneFields(ctx context.Context, query bson.M, sel bson.M, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).Select(sel).One(o)
}

func dbS3FindAllFields(ctx context.Context, query bson.M, sel bson.M, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).Select(sel).All(o)
}

func dbS3FindAllSorted(ctx context.Context, query bson.M, sort string, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).Sort(sort).All(o)
}

func dbS3IterAllSorted(ctx context.Context, query bson.M, sort string, o interface{}) *mgo.Iter {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).Sort(sort).Iter()
}

func dbS3FindOneTop(ctx context.Context, query bson.M, sort string, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).Sort(sort).Limit(1).One(o)
}

func dbS3FindAll(ctx context.Context, query bson.M, o interface{}) (error) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Find(query).All(o)
}

func dbS3FindAllInactive(ctx context.Context, o interface{}) (error) {
	states := bson.M{ "$in": []uint32{ S3StateNone, S3StateInactive } }
	query := bson.M{ "state": states }

	return dbS3FindAll(ctx, query, o)
}

func dbS3Pipe(ctx context.Context, o interface{}, pipeline interface{}) (*mgo.Pipe) {
	return Dbs(ctx).DB(s3mgo.DBName).C(dbColl(o)).Pipe(pipeline)
}
