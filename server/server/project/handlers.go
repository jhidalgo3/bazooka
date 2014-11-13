package project

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"bitbucket.org/bywan/bazooka-api/bazooka-server/server/context"
	lib "github.com/bazooka-ci/bazooka-lib"
	docker "github.com/bywan/go-dockercommand"
	"github.com/gorilla/mux"
)

const (
	buildFolderPattern = "%s/build/%s/%s" // /$bzk_home/build/$projectId/$buildId
	logFolderPattern   = "%s/log/%s/%s"   // $bzk_home/log/$projectId/$buildId
	// keyFolderPattern   = "%s/key/%s"         // $bzk_home/key/$keyName
)

type Handlers struct {
	mongoConnector *mongoConnector
	env            map[string]string
	dockerEndpoint string
}

func (p *Handlers) SetHandlers(r *mux.Router, serverContext context.Context) {
	p.mongoConnector = &mongoConnector{
		Database: serverContext.Database,
	}
	p.env = serverContext.Env
	p.dockerEndpoint = serverContext.DockerEndpoint

	r.HandleFunc("/", p.createProject).Methods("POST")
	r.HandleFunc("/", p.getProjects).Methods("GET")
	r.HandleFunc("/{id}", p.getProject).Methods("GET")
	r.HandleFunc("/{id}/job", p.startBuild).Methods("POST")
	r.HandleFunc("/{id}/job/", p.getJobs).Methods("GET")
	r.HandleFunc("/{id}/job/{job_id}", p.getJob).Methods("GET")
	r.HandleFunc("/{id}/job/{job_id}/logs", p.getJob).Methods("GET")
}

func (p *Handlers) createProject(res http.ResponseWriter, req *http.Request) {
	var project lib.Project

	decoder := json.NewDecoder(req.Body)
	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := decoder.Decode(&project)

	if err != nil {
		res.WriteHeader(400)
		encoder.Encode(&context.ErrorResponse{
			Code:    400,
			Message: "Unable to decode your json : " + err.Error(),
		})
		return
	}

	if len(project.ScmURI) == 0 {
		res.WriteHeader(400)
		encoder.Encode(&context.ErrorResponse{
			Code:    400,
			Message: "scm_uri is mandatory",
		})

		return
	}

	if len(project.ScmType) == 0 {
		res.WriteHeader(400)
		encoder.Encode(&context.ErrorResponse{
			Code:    400,
			Message: "scm_type is mandatory",
		})

		return
	}

	existantProject, err := p.mongoConnector.GetProject(project.ScmType, project.ScmURI)
	if err != nil {
		if err.Error() != "not found" {
			context.WriteError(err, res, encoder)
			return
		}
	}

	if len(existantProject.ScmURI) > 0 {
		res.WriteHeader(409)
		encoder.Encode(&context.ErrorResponse{
			Code:    409,
			Message: "scm_uri is already known",
		})

		return
	}

	// TODO : validate scm_type
	// TODO : validate data by scm_type

	err = p.mongoConnector.AddProject(&project)
	res.Header().Set("Location", "/project/"+project.ID)

	res.WriteHeader(201)
	encoder.Encode(&project)

}

func (p *Handlers) getProject(res http.ResponseWriter, req *http.Request) {
	params := mux.Vars(req)

	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	project, err := p.mongoConnector.GetProjectById(params["id"])
	if err != nil {
		if err.Error() != "not found" {
			context.WriteError(err, res, encoder)
			return
		}
		res.WriteHeader(404)
		encoder.Encode(&context.ErrorResponse{
			Code:    404,
			Message: "project not found",
		})

		return
	}

	res.WriteHeader(200)
	encoder.Encode(&project)
}

func (p *Handlers) getProjects(res http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	projects, err := p.mongoConnector.GetProjects()
	if err != nil {
		context.WriteError(err, res, encoder)
		return
	}

	res.WriteHeader(200)
	encoder.Encode(&projects)
}

func (p *Handlers) startBuild(res http.ResponseWriter, req *http.Request) {
	params := mux.Vars(req)
	var startJob lib.StartJob

	decoder := json.NewDecoder(req.Body)
	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := decoder.Decode(&startJob)
	if err != nil {
		res.WriteHeader(400)
		encoder.Encode(&context.ErrorResponse{
			Code:    400,
			Message: "Invalid body : " + err.Error(),
		})
		return
	}

	if len(startJob.ScmReference) == 0 {
		res.WriteHeader(400)
		encoder.Encode(&context.ErrorResponse{
			Code:    400,
			Message: "reference is mandatory",
		})

		return
	}

	project, err := p.mongoConnector.GetProjectById(params["id"])
	if err != nil {
		context.WriteError(err, res, encoder)
		return
	}

	client, err := docker.NewDocker(p.dockerEndpoint)
	if err != nil {
		context.WriteError(err, res, encoder)
		return
	}

	var runningJob lib.Job
	runningJob.ID = strconv.FormatInt(time.Now().Unix(), 10)
	runningJob.ProjectID = project.ID

	logFolder := fmt.Sprintf(logFolderPattern, context.BazookaHome, runningJob.ProjectID, runningJob.ID)
	os.MkdirAll(logFolder, 0666)

	logFileWriter, err := os.Create(logFolder + "/job.log")
	if err != nil {
		panic(err)
	}

	buildFolder := fmt.Sprintf(buildFolderPattern, p.env[context.BazookaEnvHome], runningJob.ProjectID, runningJob.ID)
	orchestrationEnv := map[string]string{
		"BZK_SCM":           "git",
		"BZK_SCM_URL":       project.ScmURI,
		"BZK_SCM_REFERENCE": startJob.ScmReference,
		"BZK_SCM_KEYFILE":   p.env[context.BazookaEnvSCMKeyfile], //TODO use keyfile per project
		"BZK_HOME":          buildFolder,
		"BZK_PROJECT_ID":    project.ID,
		"BZK_JOB_ID":        runningJob.ID, // TODO handle job number and tasks and save it
		"BZK_DOCKERSOCK":    p.env[context.BazookaEnvDockerSock],
	}

	container, err := client.Run(&docker.RunOptions{
		Image:       "bazooka/orchestration",
		VolumeBinds: []string{fmt.Sprintf("%s:/bazooka", buildFolder), fmt.Sprintf("%s:/var/run/docker.sock", p.env[context.BazookaEnvDockerSock])},
		Env:         orchestrationEnv,
		Detach:      true,
	})

	runningJob.OrchestrationID = container.ID()
	orchestrationLog := log.New(logFileWriter, "", log.LstdFlags)
	orchestrationLog.Printf("Start job %s on project %s with container %s\n", runningJob.ID, runningJob.ProjectID, runningJob.OrchestrationID)

	if err != nil {
		orchestrationLog.Println(err.Error())
		context.WriteError(err, res, encoder)
		return
	}

	r, w := io.Pipe()
	container.StreamLogs(w)
	go func(reader io.Reader, logFileWriter *os.File) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			orchestrationLog.Printf("%s \n", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			orchestrationLog.Println("There was an error with the scanner in attached container", err)
		}
		logFileWriter.Close()
	}(r, logFileWriter)

	err = p.mongoConnector.AddJob(&runningJob)
	res.Header().Set("Location", "/project/"+project.ID+"/job/"+runningJob.ID)

	res.WriteHeader(202)
	encoder.Encode(&runningJob)
}

func (p *Handlers) getJob(res http.ResponseWriter, req *http.Request) {
	params := mux.Vars(req)

	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	job, err := p.mongoConnector.GetJobByID(params["job_id"])
	if err != nil {
		if err.Error() != "not found" {
			context.WriteError(err, res, encoder)
			return
		}
		res.WriteHeader(404)
		encoder.Encode(&context.ErrorResponse{
			Code:    404,
			Message: "job not found",
		})
		return
	}

	// TODO: Check projectID in jobID matches the one in the request, if not
	// return 404

	res.WriteHeader(200)
	encoder.Encode(&job)
}

func (p *Handlers) getJobs(res http.ResponseWriter, req *http.Request) {
	encoder := json.NewEncoder(res)
	res.Header().Set("Content-Type", "application/json; charset=utf-8")

	jobs, err := p.mongoConnector.GetJobs()
	if err != nil {
		context.WriteError(err, res, encoder)
		return
	}

	res.WriteHeader(200)
	encoder.Encode(&jobs)
}