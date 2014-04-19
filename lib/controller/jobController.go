package controller

import (
	"fmt"
	"github.com/MG-RAST/AWE/lib/conf"
	"github.com/MG-RAST/AWE/lib/core"
	e "github.com/MG-RAST/AWE/lib/errors"
	"github.com/MG-RAST/AWE/lib/foreign/taverna"
	"github.com/MG-RAST/AWE/lib/logger"
	"github.com/MG-RAST/AWE/lib/logger/event"
	"github.com/MG-RAST/AWE/lib/request"
	"github.com/MG-RAST/golib/goweb"
	"labix.org/v2/mgo/bson"
	"net/http"
	"strconv"
	"strings"
)

type JobController struct{}

func handleAuthError(err error, cx *goweb.Context) {
	switch err.Error() {
	case e.MongoDocNotFound:
		cx.RespondWithErrorMessage("Invalid username or password", http.StatusBadRequest)
		return
		//	case e.InvalidAuth:
		//		cx.RespondWithErrorMessage("Invalid Authorization header", http.StatusBadRequest)
		//		return
	}
	logger.Error("Error at Auth: " + err.Error())
	cx.RespondWithError(http.StatusInternalServerError)
	return
}

// OPTIONS: /job
func (cr *JobController) Options(cx *goweb.Context) {
	LogRequest(cx.Request)
	cx.RespondWithOK()
	return
}

// POST: /job
func (cr *JobController) Create(cx *goweb.Context) {
	// Log Request and check for Auth
	LogRequest(cx.Request)

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	// Parse uploaded form
	params, files, err := ParseMultipartForm(cx.Request)

	if err != nil {
		if err.Error() == "request Content-Type isn't multipart/form-data" {
			cx.RespondWithErrorMessage("No job file is submitted", http.StatusBadRequest)
		} else {
			// Some error other than request encoding. Theoretically
			// could be a lost db connection between user lookup and parsing.
			// Blame the user, Its probaby their fault anyway.
			logger.Error("Error parsing form: " + err.Error())
			cx.RespondWithError(http.StatusBadRequest)
		}
		return
	}

	_, has_upload := files["upload"]
	_, has_awf := files["awf"]

	if !has_upload && !has_awf {
		cx.RespondWithErrorMessage("No job script or awf is submitted", http.StatusBadRequest)
		return
	}

	//send job submission request and get back an assigned job number (jid)
	var jid string
	jid, err = core.QMgr.JobRegister()
	if err != nil {
		logger.Error("Err@job_Create:GetNextJobNum: " + err.Error())
		cx.RespondWithErrorMessage(err.Error(), http.StatusBadRequest)
		return
	}

	var job *core.Job
	job, err = core.CreateJobUpload(params, files, jid)
	if err != nil {
		logger.Error("Err@job_Create:CreateJobUpload: " + err.Error())
		cx.RespondWithErrorMessage(err.Error(), http.StatusBadRequest)
		return
	}

	if token, err := request.RetrieveToken(cx.Request); err == nil {
		job.SetDataToken(token)
	}

	core.QMgr.EnqueueTasksByJobId(job.Id, job.TaskList())

	//log event about job submission (JB)
	logger.Event(event.JOB_SUBMISSION, "jobid="+job.Id+";jid="+job.Jid+";name="+job.Info.Name+";project="+job.Info.Project+";user="+job.Info.User)
	cx.RespondWithData(job)
	return
}

// GET: /job/{id}
func (cr *JobController) Read(id string, cx *goweb.Context) {
	LogRequest(cx.Request)

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	// Gather query params
	query := &Query{Li: cx.Request.URL.Query()}

	if query.Has("perf") {
		//Load job perf by id
		perf, err := core.LoadJobPerf(id)
		if err != nil {
			if err.Error() == e.MongoDocNotFound {
				cx.RespondWithNotFound()
				return
			} else {
				// In theory the db connection could be lost between
				// checking user and load but seems unlikely.
				logger.Error("Err@LoadJobPerf: " + id + ":" + err.Error())
				cx.RespondWithErrorMessage("job perf stats not found:"+id, http.StatusBadRequest)
				return
			}
		}
		cx.RespondWithData(perf)
		return //done with returning perf, no need to load job further.
	}

	// Load job by id
	job, err := core.LoadJob(id)

	if err != nil {
		if err.Error() == e.MongoDocNotFound {
			cx.RespondWithNotFound()
			return
		} else {
			// In theory the db connection could be lost between
			// checking user and load but seems unlikely.
			// logger.Error("Err@job_Read:LoadJob: " + id + ":" + err.Error())
			cx.RespondWithErrorMessage("job not found:"+id, http.StatusBadRequest)
			return
		}
	}

	if core.QMgr.IsJobRegistered(id) {
		job.Registered = true
	} else {
		job.Registered = false
	}

	if query.Has("export") {
		target := query.Value("export")
		if target == "" {
			cx.RespondWithErrorMessage("lacking stage id from which the recompute starts", http.StatusBadRequest)
		} else if target == "taverna" {
			wfrun, err := taverna.ExportWorkflowRun(job)
			if err != nil {
				cx.RespondWithErrorMessage("failed to export job to taverna workflowrun:"+id, http.StatusBadRequest)
			}
			cx.RespondWithData(wfrun)
			return
		}
	}

	// Base case respond with job in json
	cx.RespondWithData(job)
	return
}

// GET: /job
// To do:
// - Iterate job queries
func (cr *JobController) ReadMany(cx *goweb.Context) {
	LogRequest(cx.Request)

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	// Gather query params
	query := &Query{Li: cx.Request.URL.Query()}

	// Setup query and jobs objects
	q := bson.M{}
	jobs := core.Jobs{}

	limit := conf.DEFAULT_PAGE_SIZE
	offset := 0
	order := "updatetime"
	direction := "desc"

	if query.Has("limit") {
		limit, _ = strconv.Atoi(query.Value("limit"))
	}
	if query.Has("offset") {
		offset, _ = strconv.Atoi(query.Value("offset"))
	}
	if query.Has("order") {
		order = query.Value("order")
	}
	if query.Has("direction") {
		direction = query.Value("direction")
	}

	// Gather params to make db query. Do not include the
	// following list.
	skip := map[string]int{"limit": 1,
		"offset":     1,
		"query":      1,
		"recent":     1,
		"order":      1,
		"direction":  1,
		"active":     1,
		"suspend":    1,
		"registered": 1,
	}
	if query.Has("query") {
		for key, val := range query.All() {
			_, s := skip[key]
			if !s {
				queryvalues := strings.Split(val[0], ",")
				q[key] = bson.M{"$in": queryvalues}
			}
		}
	} else if query.Has("active") {
		q["state"] = bson.M{"$in": core.JOB_STATS_ACTIVE}
	} else if query.Has("suspend") {
		q["state"] = core.JOB_STAT_SUSPEND
	}

	//getting real active (in-progress) job (some jobs are in "submitted" states but not in the queue,
	//because they may have failed and not recovered from the mongodb).
	if query.Has("active") {
		err := jobs.GetAll(q, order, direction)
		if err != nil {
			logger.Error("err " + err.Error())
			cx.RespondWithError(http.StatusBadRequest)
			return
		}

		filtered_jobs := []core.Job{}
		act_jobs := core.QMgr.GetActiveJobs()
		length := jobs.Length()

		skip := 0
		count := 0
		for i := 0; i < length; i++ {
			job := jobs.GetJobAt(i)
			if _, ok := act_jobs[job.Id]; ok {
				if skip < offset {
					skip += 1
					continue
				}
				job.Registered = true
				filtered_jobs = append(filtered_jobs, job)
				count += 1
				if count == limit {
					break
				}
			}
		}
		cx.RespondWithPaginatedData(filtered_jobs, limit, offset, len(act_jobs))
		return
	}

	//geting suspended job in the current queue (excluding jobs in db but not in qmgr)
	if query.Has("suspend") {
		err := jobs.GetAll(q, order, direction)
		if err != nil {
			logger.Error("err " + err.Error())
			cx.RespondWithError(http.StatusBadRequest)
			return
		}

		filtered_jobs := []core.Job{}
		suspend_jobs := core.QMgr.GetSuspendJobs()
		length := jobs.Length()

		skip := 0
		count := 0
		for i := 0; i < length; i++ {
			job := jobs.GetJobAt(i)
			if _, ok := suspend_jobs[job.Id]; ok {
				if skip < offset {
					skip += 1
					continue
				}
				job.Registered = true
				filtered_jobs = append(filtered_jobs, job)
				count += 1
				if count == limit {
					break
				}
			}
		}
		cx.RespondWithPaginatedData(filtered_jobs, limit, offset, len(suspend_jobs))
		return
	}

	if query.Has("registered") {
		err := jobs.GetAll(q, order, direction)
		if err != nil {
			logger.Error("err " + err.Error())
			cx.RespondWithError(http.StatusBadRequest)
			return
		}

		paged_jobs := []core.Job{}
		registered_jobs := []core.Job{}
		length := jobs.Length()

		total := 0
		for i := 0; i < length; i++ {
			job := jobs.GetJobAt(i)
			if core.QMgr.IsJobRegistered(job.Id) {
				job.Registered = true
				registered_jobs = append(registered_jobs, job)
				total += 1
			}
		}
		count := 0
		for i := offset; i < len(registered_jobs); i++ {
			paged_jobs = append(paged_jobs, registered_jobs[i])
			count += 1
			if count == limit {
				break
			}
		}
		cx.RespondWithPaginatedData(paged_jobs, limit, offset, total)
		return
	}

	total, err := jobs.GetPaginated(q, limit, offset, order, direction)
	if err != nil {
		logger.Error("err " + err.Error())
		cx.RespondWithError(http.StatusBadRequest)
		return
	}
	filtered_jobs := []core.Job{}
	length := jobs.Length()
	for i := 0; i < length; i++ {
		job := jobs.GetJobAt(i)
		if core.QMgr.IsJobRegistered(job.Id) {
			job.Registered = true
		} else {
			job.Registered = false
		}
		filtered_jobs = append(filtered_jobs, job)
	}
	cx.RespondWithPaginatedData(filtered_jobs, limit, offset, total)
	return

}

// PUT: /job
func (cr *JobController) UpdateMany(cx *goweb.Context) {
	LogRequest(cx.Request)

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	// Gather query params
	query := &Query{Li: cx.Request.URL.Query()}
	if query.Has("resumeall") { //resume the suspended job
		num := core.QMgr.ResumeSuspendedJobs()
		cx.RespondWithData(fmt.Sprintf("%d suspended jobs resumed", num))
		return
	}
	cx.RespondWithError(http.StatusNotImplemented)
}

// PUT: /job/{id} -> used for job manipulation
func (cr *JobController) Update(id string, cx *goweb.Context) {
	// Log Request and check for Auth
	LogRequest(cx.Request)

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	// Gather query params
	query := &Query{Li: cx.Request.URL.Query()}
	if query.Has("resume") { // to resume a suspended job
		if err := core.QMgr.ResumeSuspendedJob(id); err != nil {
			cx.RespondWithErrorMessage("fail to resume job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job resumed: " + id)
		return
	}
	if query.Has("suspend") { // to suspend an in-progress job
		if err := core.QMgr.SuspendJob(id, "manually suspended"); err != nil {
			cx.RespondWithErrorMessage("fail to suspend job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job suspended: " + id)
		return
	}
	if query.Has("resubmit") || query.Has("reregister") { // to re-submit a job from mongodb
		if err := core.QMgr.ResubmitJob(id); err != nil {
			cx.RespondWithErrorMessage("fail to resubmit job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job resubmitted: " + id)
		return
	}
	if query.Has("recompute") { // to recompute a job from task i, the successive/downstream tasks of i will all be computed
		stage := query.Value("recompute")
		if stage == "" {
			cx.RespondWithErrorMessage("lacking stage id from which the recompute starts", http.StatusBadRequest)
		}
		if err := core.QMgr.RecomputeJob(id, stage); err != nil {
			cx.RespondWithErrorMessage("fail to recompute job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job recompute started: " + id)
		return
	}
	if query.Has("clientgroup") { // change the clientgroup attribute of the job
		newgroup := query.Value("clientgroup")
		if newgroup == "" {
			cx.RespondWithErrorMessage("lacking groupname", http.StatusBadRequest)
		}
		if err := core.QMgr.UpdateGroup(id, newgroup); err != nil {
			cx.RespondWithErrorMessage("fail to update group for job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job group updated: " + id + " to " + newgroup)
		return
	}
	if query.Has("priority") { // change the clientgroup attribute of the job
		priority_str := query.Value("priority")
		if priority_str == "" {
			cx.RespondWithErrorMessage("lacking priority value (0-3)", http.StatusBadRequest)
		}
		priority, err := strconv.Atoi(priority_str)
		if err != nil {
			cx.RespondWithErrorMessage("need int for priority value (0-3)", http.StatusBadRequest)
		}
		if err := core.QMgr.UpdatePriority(id, priority); err != nil {
			cx.RespondWithErrorMessage("fail to priority for job: "+id+" "+err.Error(), http.StatusBadRequest)
		}
		cx.RespondWithData("job priority updated: " + id + " to " + priority_str)
		return
	}

	if query.Has("settoken") { // set data token
		token, err := request.RetrieveToken(cx.Request)
		if err != nil {
			cx.RespondWithErrorMessage("fail to retrieve token for job, pls set token in header: "+id+" "+err.Error(), http.StatusBadRequest)
			return
		}

		job, err := core.LoadJob(id)
		if err != nil {
			if err.Error() == e.MongoDocNotFound {
				cx.RespondWithNotFound()
			} else {
				logger.Error("Err@job_Read:LoadJob: " + id + ":" + err.Error())
				cx.RespondWithErrorMessage("job not found:"+id, http.StatusBadRequest)
			}
			return
		}
		job.SetDataToken(token)
		cx.RespondWithData("data token set for job: " + id)
		return
	}

	cx.RespondWithData("requested job operation not supported")
	return
}

// DELETE: /job/{id}
func (cr *JobController) Delete(id string, cx *goweb.Context) {

	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	LogRequest(cx.Request)
	if err := core.QMgr.DeleteJob(id); err != nil {
		cx.RespondWithErrorMessage("fail to delete job: "+id, http.StatusBadRequest)
		return
	}
	cx.RespondWithData("job deleted: " + id)
	return
}

// DELETE: /job?suspend
func (cr *JobController) DeleteMany(cx *goweb.Context) {
	if conf.ADMIN_AUTH {
		if AdminAuthenticated(cx) == false {
			return
		}
	}

	LogRequest(cx.Request)
	// Gather query params
	query := &Query{Li: cx.Request.URL.Query()}
	if query.Has("suspend") {
		num := core.QMgr.DeleteSuspendedJobs()
		cx.RespondWithData(fmt.Sprintf("deleted %d suspended jobs", num))
	} else if query.Has("zombie") {
		num := core.QMgr.DeleteZombieJobs()
		cx.RespondWithData(fmt.Sprintf("deleted %d zombie jobs", num))
	} else {
		cx.RespondWithError(http.StatusNotImplemented)
	}
	return
}
