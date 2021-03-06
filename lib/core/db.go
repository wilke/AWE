package core

import (
	"errors"
	"fmt"
	"github.com/MG-RAST/AWE/lib/acl"
	"github.com/MG-RAST/AWE/lib/conf"
	"github.com/MG-RAST/AWE/lib/core/cwl"
	"github.com/MG-RAST/AWE/lib/db"
	"github.com/MG-RAST/AWE/lib/logger"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
)

// mongodb has hard limit of 16 MB docuemnt size
var DocumentMaxByte = 16777216

// indexed info fields for search
var JobInfoIndexes = []string{"name", "submittime", "completedtime", "pipeline", "clientgroups", "project", "service", "user", "priority", "userattr.submission"}

type StructContainer struct {
	Data interface{} `json:"data"`
}

func HasInfoField(a string) bool {
	for _, b := range JobInfoIndexes {
		if b == a {
			return true
		}
	}
	return false
}

func InitJobDB() {
	session := db.Connection.Session.Copy()
	defer session.Close()
	cj := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	cj.EnsureIndex(mgo.Index{Key: []string{"acl.owner"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"acl.read"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"acl.write"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"acl.delete"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"id"}, Unique: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"state"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"expiration"}, Background: true})
	cj.EnsureIndex(mgo.Index{Key: []string{"updatetime"}, Background: true})
	for _, v := range JobInfoIndexes {
		cj.EnsureIndex(mgo.Index{Key: []string{"info." + v}, Background: true})
	}
	cp := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_PERF)
	cp.EnsureIndex(mgo.Index{Key: []string{"id"}, Unique: true})
}

func InitClientGroupDB() {
	session := db.Connection.Session.Copy()
	defer session.Close()
	cc := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	cc.EnsureIndex(mgo.Index{Key: []string{"id"}, Unique: true})
	cc.EnsureIndex(mgo.Index{Key: []string{"name"}, Unique: true})
	cc.EnsureIndex(mgo.Index{Key: []string{"token"}, Unique: true})
}

func dbDelete(q bson.M, coll string) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(coll)
	_, err = c.RemoveAll(q)
	return
}

func dbUpsert(t interface{}) (err error) {
	// test that document not to large
	if nbson, err := bson.Marshal(t); err == nil {
		if len(nbson) >= DocumentMaxByte {
			return errors.New(fmt.Sprintf("bson document size is greater than limit of %d bytes", DocumentMaxByte))
		}
	}
	session := db.Connection.Session.Copy()
	defer session.Close()
	switch t := t.(type) {
	case *Job:
		c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
		_, err = c.Upsert(bson.M{"id": t.Id}, &t)
	case *JobPerf:
		c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_PERF)
		_, err = c.Upsert(bson.M{"id": t.Id}, &t)
	case *ClientGroup:
		c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
		_, err = c.Upsert(bson.M{"id": t.Id}, &t)
	default:
		fmt.Printf("invalid database entry type\n")
	}
	return
}

func dbCount(q bson.M) (count int, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	if count, err = c.Find(q).Count(); err != nil {
		return 0, err
	} else {
		return count, nil
	}
}

func dbFind(q bson.M, results *Jobs, options map[string]int) (count int, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	query := c.Find(q)
	if count, err = query.Count(); err != nil {
		return 0, err
	}
	if limit, has := options["limit"]; has {
		if offset, has := options["offset"]; has {
			err = query.Limit(limit).Skip(offset).All(results)
			if results != nil {
				_, err = results.Init()
				if err != nil {
					return
				}
			}
			return
		} else {
			return 0, errors.New("store.db.Find options limit and offset must be used together")
		}
	}
	err = query.All(results)
	if results != nil {
		_, err = results.Init()
		if err != nil {
			return
		}
	}
	return
}

// get a minimal subset of the job documents required for an admin overview
// for all completed jobs younger than a month and all running jobs
func dbAdminData(special string) (data []interface{}, err error) {
	// get a DB connection
	session := db.Connection.Session.Copy()

	// close the connection when the function completes
	defer session.Close()

	// set the database and collection
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	// get the completed jobs that have a completed time not older than one month
	var completedjobs = bson.M{"state": "completed", "info.completedtime": bson.M{"$gt": time.Now().AddDate(0, -1, 0)}}

	// get all runnning jobs (those not deleted and not completed)
	var runningjobs = bson.M{"state": bson.M{"$nin": []string{"completed", "deleted"}}}

	// select only those fields required for the output
	var resultfields = bson.M{"_id": 0, "state": 1, "info.name": 1, "info.submittime": 1, "info.startedtime": 1, "info.completedtime": 1, "info.pipeline": 1, "tasks.createdDate": 1, "tasks.startedDate": 1, "tasks.completedDate": 1, "tasks.state": 1, "tasks.inputs.size": 1, "tasks.outputs.size": 1, special: 1}

	// return all data without iterating
	err = c.Find(bson.M{"$or": []bson.M{completedjobs, runningjobs}}).Select(resultfields).All(&data)

	return
}

func dbFindSort(q bson.M, results *Jobs, options map[string]int, sortby string, do_init bool) (count int, err error) {
	if sortby == "" {
		return 0, errors.New("sortby must be an nonempty string")
	}
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	query := c.Find(q)
	if count, err = query.Count(); err != nil {
		return 0, err
	}

	if limit, has := options["limit"]; has {
		if offset, has := options["offset"]; has {
			err = query.Sort(sortby).Limit(limit).Skip(offset).All(results)
			if results != nil {
				if do_init {
					_, err = results.Init()
					if err != nil {
						return
					}
				}
			}
			return
		}
	}
	err = query.Sort(sortby).All(results)
	if err != nil {
		err = fmt.Errorf("query.Sort(sortby).All(results) failed: %s", err.Error())
		return
	}
	if results == nil {
		err = fmt.Errorf("(dbFindSort) results == nil")
		return
	}

	var count_changed int

	if do_init {
		count_changed, err = results.Init()
		if err != nil {
			err = fmt.Errorf("(dbFindSort) results.Init() failed: %s", err.Error())
			return
		}
		logger.Debug(1, "%d jobs haven been updated by Init function", count_changed)
	}
	return
}

func DbFindDistinct(q bson.M, d string) (results interface{}, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	err = c.Find(q).Distinct("info."+d, &results)
	return
}

func dbFindClientGroups(q bson.M, results *ClientGroups) (count int, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	query := c.Find(q)
	if count, err = query.Count(); err != nil {
		return 0, err
	}
	err = query.All(results)
	return
}

func dbFindSortClientGroups(q bson.M, results *ClientGroups, options map[string]int, sortby string) (count int, err error) {
	if sortby == "" {
		return 0, errors.New("sortby must be an nonempty string")
	}
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	query := c.Find(q)
	if count, err = query.Count(); err != nil {
		return 0, err
	}

	if limit, has := options["limit"]; has {
		if offset, has := options["offset"]; has {
			err = query.Sort(sortby).Limit(limit).Skip(offset).All(results)
			return
		}
	}
	err = query.Sort(sortby).All(results)
	return
}

func dbUpdateJobState(job_id string, newState string, notes string) (err error) {
	logger.Debug(3, "(dbUpdateJobState) job_id: %s", job_id)
	var update_value bson.M

	if newState == JOB_STAT_COMPLETED {
		update_value = bson.M{"state": newState, "notes": notes, "info.completedtime": time.Now()}
	} else {
		update_value = bson.M{"state": newState, "notes": notes}
	}
	return dbUpdateJobFields(job_id, update_value)
}

func dbGetJobTasks(job_id string) (tasks []*Task, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	selector := bson.M{"id": job_id}
	fieldname := "tasks"
	err = c.Find(selector).Select(bson.M{fieldname: 1}).All(&tasks)
	if err != nil {
		err = fmt.Errorf("(dbGetJobTasks) Error getting tasks from job_id %s: %s", job_id, err.Error())
		return
	}
	return
}

func dbGetJobTaskString(job_id string, task_id string, fieldname string) (result string, err error) {
	var cont StructContainer
	err = dbGetJobTaskField(job_id, task_id, fieldname, &cont)
	if err != nil {
		return
	}
	result = cont.Data.(string)
	return
}

func dbGetJobTaskTime(job_id string, task_id string, fieldname string) (result time.Time, err error) {
	var cont StructContainer
	err = dbGetJobTaskField(job_id, task_id, fieldname, &cont)
	if err != nil {
		return
	}
	result = cont.Data.(time.Time)
	return
}

func dbGetJobTaskInt(job_id string, task_id string, fieldname string) (result int, err error) {
	var cont StructContainer
	err = dbGetJobTaskField(job_id, task_id, fieldname, &cont)
	if err != nil {
		return
	}
	result = cont.Data.(int)
	return
}

func dbGetJobWorkflow_InstanceInt(job_id string, task_id string, fieldname string) (result int, err error) {
	var cont StructContainer
	err = dbGetJobWorkflow_InstanceField(job_id, task_id, fieldname, &cont)
	if err != nil {
		return
	}
	result = cont.Data.(int)
	return
}

func dbGetJobWorkflow_InstanceField(job_id string, task_id string, fieldname string, result *StructContainer) (err error) {

	array_name := "workflow_instances"
	id_field := "id"
	return dbGetJobArrayField(job_id, task_id, array_name, id_field, fieldname, result)
}

func dbGetJobTaskField(job_id string, task_id string, fieldname string, result *StructContainer) (err error) {

	array_name := "tasks"
	id_field := "taskid"
	return dbGetJobArrayField(job_id, task_id, array_name, id_field, fieldname, result)
}

// TODO: warning: this does not cope with subfields such as "partinfo.index"
func dbGetJobArrayField(job_id string, task_id string, array_name string, id_field string, fieldname string, result *StructContainer) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	projection := bson.M{array_name: bson.M{"$elemMatch": bson.M{id_field: task_id}}, array_name + "." + fieldname: 1}
	temp_result := bson.M{}

	err = c.Find(selector).Select(projection).One(&temp_result)
	if err != nil {
		err = fmt.Errorf("(dbGetJobArrayField) Error getting field from job_id %s , %s=%s and fieldname %s: %s", job_id, id_field, task_id, fieldname, err.Error())
		return
	}

	//logger.Debug(3, "GOT: %v", temp_result)

	tasks_unknown := temp_result[array_name]
	tasks, ok := tasks_unknown.([]interface{})

	if !ok {
		err = fmt.Errorf("(dbGetJobArrayField) Array expected, but not found")
		return
	}
	if len(tasks) == 0 {
		err = fmt.Errorf("(dbGetJobArrayField) result task array empty")
		return
	}

	first_task := tasks[0].(bson.M)
	test_result, ok := first_task[fieldname]

	if !ok {
		err = fmt.Errorf("(dbGetJobArrayField) Field %s not in task object", fieldname)
		return
	}
	result.Data = test_result

	//logger.Debug(3, "GOT2: %v", result)
	logger.Debug(3, "(dbGetJobArrayField) %s got something", fieldname)
	return
}

func dbGetJobTask(job_id string, task_id string) (result *Task, err error) {
	dummy_job := NewJob()
	dummy_job.Init()

	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	selector := bson.M{"id": job_id}
	projection := bson.M{"tasks": bson.M{"$elemMatch": bson.M{"taskid": task_id}}}

	err = c.Find(selector).Select(projection).One(&dummy_job)
	if err != nil {
		err = fmt.Errorf("(dbGetJobTask) Error getting field from job_id %s , task_id %s : %s", job_id, task_id, err.Error())
		return
	}
	if len(dummy_job.Tasks) != 1 {
		err = fmt.Errorf("(dbGetJobTask) len(dummy_job.Tasks) != 1   len(dummy_job.Tasks)=%d", len(dummy_job.Tasks))
		return
	}

	result = dummy_job.Tasks[0]
	return
}

func dbGetPrivateEnv(job_id string, task_id string) (result map[string]string, err error) {
	task, err := dbGetJobTask(job_id, task_id)
	if err != nil {
		return
	}
	result = task.Cmd.Environ.Private
	for key, val := range result {
		logger.Debug(3, "(dbGetPrivateEnv) got %s=%s ", key, val)
	}
	return
}

func dbGetJobFieldInt(job_id string, fieldname string) (result int, err error) {
	err = dbGetJobField(job_id, fieldname, &result)
	if err != nil {
		return
	}
	return
}

func dbGetJobFieldString(job_id string, fieldname string) (result string, err error) {
	err = dbGetJobField(job_id, fieldname, &result)
	logger.Debug(3, "(dbGetJobFieldString) result: %s", result)
	if err != nil {
		return
	}
	return
}

func dbGetJobField(job_id string, fieldname string, result interface{}) (err error) {

	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	err = c.Find(selector).Select(bson.M{fieldname: 1}).One(&result)
	if err != nil {
		err = fmt.Errorf("(dbGetJobField) Error getting field from job_id %s and fieldname %s: %s", job_id, fieldname, err.Error())
		return
	}
	return
}

type Job_Acl struct {
	Acl acl.Acl `bson:"acl" json:"-"`
}

func DBGetJobAcl(job_id string) (_acl acl.Acl, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	job := Job_Acl{}

	err = c.Find(selector).Select(bson.M{"acl": 1}).One(&job)
	if err != nil {
		err = fmt.Errorf("Error getting acl field from job_id %s: %s", job_id, err.Error())
		return
	}

	_acl = job.Acl
	return
}

func dbGetJobFieldTime(job_id string, fieldname string) (result time.Time, err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	err = c.Find(selector).Select(bson.M{fieldname: 1}).One(&result)
	if err != nil {
		err = fmt.Errorf("(dbGetJobFieldTime) Error getting field from job_id %s and fieldname %s: %s", job_id, fieldname, err.Error())
		return
	}
	return
}

func dbPushJobTask(job_id string, task *Task) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	change := bson.M{"$push": bson.M{"tasks": task}}

	err = c.Update(selector, change)
	if err != nil {
		err = fmt.Errorf("Error adding task: " + err.Error())
		return
	}
	return
}

func dbPushJobWorkflowInstance(job_id string, wi *WorkflowInstance) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	change := bson.M{"$push": bson.M{"workflow_instances": wi}}

	err = c.Update(selector, change)
	if err != nil {
		err = fmt.Errorf("Error adding WorkflowInstance: " + err.Error())
		return
	}
	return
}

func dbUpdateJobWorkflow_instancesFieldOutputs(job_id string, subworkflow_id string, outputs cwl.Job_document) (err error) {
	update_value := bson.M{"workflow_instances.$.outputs": outputs}
	return dbUpdateJobWorkflow_instancesFields(job_id, subworkflow_id, update_value)
}

func dbUpdateJobWorkflow_instancesFieldInt(job_id string, subworkflow_id string, fieldname string, value int) (err error) {
	update_value := bson.M{"workflow_instances.$." + fieldname: value}
	err = dbUpdateJobWorkflow_instancesFields(job_id, subworkflow_id, update_value)
	if err != nil {
		err = fmt.Errorf("(dbUpdateJobWorkflow_instancesFieldInt) (subworkflow_id: %s, fieldname: %s, value: %d) %s", subworkflow_id, fieldname, value, err.Error())
		return
	}
	return
}

func dbUpdateJobWorkflow_instancesField(job_id string, subworkflow_id string, fieldname string, value interface{}) (err error) {
	update_value := bson.M{"workflow_instances.$." + fieldname: value}
	return dbUpdateJobWorkflow_instancesFields(job_id, subworkflow_id, update_value)
}

func dbUpdateJobWorkflow_instancesFields(job_id string, subworkflow_id string, update_value bson.M) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id, "workflow_instances.id": subworkflow_id}

	err = c.Update(selector, bson.M{"$set": update_value})
	if err != nil {
		err = fmt.Errorf("(dbUpdateJobWorkflow_instancesFields) Error updating workflow_instance: " + err.Error())
		return
	}
	return
}

func dbUpdateJobFields(job_id string, update_value bson.M) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id}

	err = c.Update(selector, bson.M{"$set": update_value})
	if err != nil {
		err = fmt.Errorf("Error updating job fields: " + err.Error())
		return
	}
	return
}

func dbUpdateJobFieldBoolean(job_id string, fieldname string, value bool) (err error) {
	update_value := bson.M{fieldname: value}
	return dbUpdateJobFields(job_id, update_value)
}

func dbUpdateJobFieldString(job_id string, fieldname string, value string) (err error) {
	update_value := bson.M{fieldname: value}
	return dbUpdateJobFields(job_id, update_value)
}

func dbUpdateJobFieldInt(job_id string, fieldname string, value int) (err error) {
	update_value := bson.M{fieldname: value}
	return dbUpdateJobFields(job_id, update_value)
}

func dbUpdateJobFieldTime(job_id string, fieldname string, value time.Time) (err error) {
	update_value := bson.M{fieldname: value}
	return dbUpdateJobFields(job_id, update_value)
}

func dbUpdateJobFieldNull(job_id string, fieldname string) (err error) {
	update_value := bson.M{fieldname: nil}
	return dbUpdateJobFields(job_id, update_value)
}

func dbUpdateJobTaskFields(job_id string, task_id string, update_value bson.M) (err error) {
	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)
	selector := bson.M{"id": job_id, "tasks.taskid": task_id}

	err = c.Update(selector, bson.M{"$set": update_value})
	if err != nil {
		err = fmt.Errorf("(dbUpdateJobTaskFields) Error updating task: " + err.Error())
		return
	}
	return
}

func dbUpdateJobTaskField(job_id string, task_id string, fieldname string, value interface{}) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)

}

func dbUpdateJobTaskInt(job_id string, task_id string, fieldname string, value int) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)

}
func dbUpdateJobTaskBoolean(job_id string, task_id string, fieldname string, value bool) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)

}

func dbUpdateJobTaskString(job_id string, task_id string, fieldname string, value string) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	err = dbUpdateJobTaskFields(job_id, task_id, update_value)

	if err != nil {
		err = fmt.Errorf(" job_id=%s, task_id=%s, fieldname=%s, value=%s error=%s", job_id, task_id, fieldname, value, err.Error())
	}
	return
}

func dbUpdateJobTaskTime(job_id string, task_id string, fieldname string, value time.Time) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)
}

func dbUpdateJobTaskPartition(job_id string, task_id string, partition *PartInfo) (err error) {
	update_value := bson.M{"tasks.$.partinfo": partition}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)
}

func dbUpdateJobTaskIO(job_id string, task_id string, fieldname string, value []*IO) (err error) {
	update_value := bson.M{"tasks.$." + fieldname: value}
	return dbUpdateJobTaskFields(job_id, task_id, update_value)
}

func dbIncrementJobTaskField(job_id string, task_id string, fieldname string, increment_value int) (err error) {

	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	selector := bson.M{"id": job_id, "tasks.taskid": task_id}

	update_value := bson.M{"tasks.$." + fieldname: increment_value}

	err = c.Update(selector, bson.M{"$inc": update_value})
	if err != nil {
		err = fmt.Errorf("Error incrementing job_id=%s fieldname=%s by %d: %s", job_id, fieldname, increment_value, err.Error())
		return
	}

	return
}

func DbUpdateJobField(job_id string, key string, value interface{}) (err error) {

	session := db.Connection.Session.Copy()
	defer session.Close()

	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	query := bson.M{"id": job_id}
	update_value := bson.M{key: value}
	update := bson.M{"$set": update_value}

	err = c.Update(query, update)
	if err != nil {
		err = fmt.Errorf("Error updating job %s and key %s in job: %s", job_id, key, err.Error())
		return
	}

	return
}

func LoadJob(id string) (job *Job, err error) {
	job = NewJob()
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_JOBS)

	err = c.Find(bson.M{"id": id}).One(&job)
	if err != nil {
		job = nil
		err = fmt.Errorf("(LoadJob) c.Find failed: %s", err.Error())
		return
	}

	changed, xerr := job.Init()
	if xerr != nil {
		err = fmt.Errorf("(LoadJob) job.Init failed: %s", xerr.Error())
		return
	}

	// To fix incomplete or inconsistent database entries
	if changed {
		job.Save()
	}

	return

}

func LoadJobPerf(id string) (perf *JobPerf, err error) {
	perf = new(JobPerf)
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_PERF)
	if err = c.Find(bson.M{"id": id}).One(&perf); err == nil {
		return perf, nil
	}
	return nil, err
}

func LoadClientGroup(id string) (clientgroup *ClientGroup, err error) {
	clientgroup = new(ClientGroup)
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	if err = c.Find(bson.M{"id": id}).One(&clientgroup); err == nil {
		return clientgroup, nil
	}
	return nil, err
}

func LoadClientGroupByName(name string) (clientgroup *ClientGroup, err error) {
	clientgroup = new(ClientGroup)
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	if err = c.Find(bson.M{"name": name}).One(&clientgroup); err == nil {
		return clientgroup, nil
	}
	return nil, err
}

func LoadClientGroupByToken(token string) (clientgroup *ClientGroup, err error) {
	clientgroup = new(ClientGroup)
	session := db.Connection.Session.Copy()
	defer session.Close()
	c := session.DB(conf.MONGODB_DATABASE).C(conf.DB_COLL_CGS)
	if err = c.Find(bson.M{"token": token}).One(&clientgroup); err == nil {
		return clientgroup, nil
	}
	return nil, err
}

func DeleteClientGroup(id string) (err error) {
	err = dbDelete(bson.M{"id": id}, conf.DB_COLL_CGS)
	return
}
