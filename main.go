package main

import (
	"database/sql"
	"io/ioutil"
	"time"

	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	_ "os"

	_ "github.com/mattn/go-sqlite3"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	path                = "/"
	defaultDBFile       = "/tmp/webbook.db"
	defaultLogFile      = "/tmp/webhook-log.txt"
	defaultGitlabSecret = "MyGitLabToken"
	//defaultGithubSecret = "MyGitHubToken"
)

type gitlabEvent struct {
	contentLength string
	eventType     string
	instance      string
	payload       string
	token         string
	userAgent     string
	eventUuid     string
	gotAllHeaders bool
}

func getEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func openOrCreateDB(dbFileName string) *sql.DB {
	dbConn, err := sql.Open("sqlite3", dbFileName) // Open the created SQLite File
	if err != nil {
		log.Fatal(err)
	}

	webhookTableDDL := `CREATE TABLE IF NOT EXISTS gitlab_webhooks(
			"id" integer NOT NULL PRIMARY KEY ASC on conflict fail AUTOINCREMENT,
			created_at integer(4) not null default (strftime('%s','now')),
			processed_at 	integer(4),	-- when this row was last processed
			gotAllHeaders 	boolean, 	-- did we get all headers?
			contentLength 	INTEGER(4), -- content length
			userAgent 		TEXT, 		-- User-Agent
			eventType 		TEXT, 		-- GitLab Event
			instance 		TEXT,  		-- Gitlab Instance
			eventUuid 		TEXT, 		-- Gitlab event UUID
			token 			TEXT, 		-- Token
			"payload" 		TEXT 		-- event payload
		);`

	pipelineTableDDL := `CREATE TABLE IF NOT EXISTS gitlab_pipeline(
			"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
			created_at 		integer(4) not null default (strftime('%s','now')),
			processed_at 	integer(4),
			event_uuid 		TEXT, 		-- Gitlab event UUID
			instance 		TEXT,  		-- Gitlab Instance
			"pod_resource" 	TEXT
		);`

	jobTableDDL := `
		CREATE TABLE IF NOT EXISTS gitlab_job(
			"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
			created_at integer(4) not null default (strftime('%s','now')),
			processed_at 	integer(4),
			event_uuid 		TEXT, 		-- Gitlab event UUID
			instance 		TEXT,  		-- Gitlab Instance
			"pod_resource" 	TEXT
		);`
	pipelineViewDDL := `
		CREATE VIEW IF NOT EXISTS pipelines AS 
			SELECT 
  				json_extract(payload, '$.object_kind') AS kind,
    			json_extract(payload, '$.object_attributes.id') as pipeline_id,
	  			json_extract(payload, '$.object_attributes.status') as status,
	    		payload
			FROM
		  		gitlab_webhooks gw
		  	WHERE 
		    	json_extract(payload, '$.object_kind') == 'pipeline';`

	statement, err := dbConn.Prepare(webhookTableDDL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec() // Execute SQL Statements

	statement, err = dbConn.Prepare(pipelineTableDDL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec()                             // Execute SQL Statements
	statement, err = dbConn.Prepare(jobTableDDL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec()                                 // Execute SQL Statements
	statement, err = dbConn.Prepare(pipelineViewDDL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec() // Execute SQL Statements
	return dbConn
}
func containsValuesInArray(values []string, array []string) bool {
	for _, v := range values {
		for _, a := range array {
			if a == v {
				return true
			}
		}
	}
	return false
}

var (
	pipelinesCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pipelines_created_total",
		Help: "All pipelines created",
	},
	// pipelinesCreatedTotal.Inc()
	)
)
var (
	stagesCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "stages_created_total",
		Help: "All stages created across all pipelines",
	},
	// stagesCreatedTotal.Inc()
	)
)
var (
	jobsCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "jobs_created_total",
		Help: "All jobs created across all pipelines",
	},
	// jobsCreatedTotal.Inc()
	)
)
var (
	pipelinesCreatedPerProject = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pipelines_created_for_project",
			Help: "Per project pipelines count",
		},
		[]string{"project", "branch"}, // Specify the label names here
		// pipelinesCreatedPerProject.WithLabelValues("value1", "value2").Inc()
	)
)

func recordMetrics() {
	go func() {
		for {
			pipelinesCreatedTotal.Inc()
			time.Sleep(60 * time.Second)
		}
	}()
}

func main() {

	// Set variables from environment
	dbFileName := getEnv("WEBBOOKS_DB_FILE", defaultDBFile)
	log.Println("Using DB file [", dbFileName, "]")
	logFileName := getEnv("WEBBOOKS_LOGS_FILE", defaultLogFile)
	log.Println("Using Log file [", logFileName, "]")
	gitlabSecret := getEnv("GITLAB_SECRET", defaultGitlabSecret)
	log.Println("Using GitLab Secret [", gitlabSecret, "]")
	//githubSecret := getEnv("GITHUB_SECRET", defaultGithubSecret)
	//log.Println("Using GitHub Secret [", githubSecret, "]")

	// Open connection to DB, OR
	// Create and initialize if does not exist,
	dbHandle := openOrCreateDB(dbFileName)
	defer dbHandle.Close()

	// Test insert
	insertSQLGitlabEvent := `
		INSERT INTO gitlab_webhooks
			(
			contentLength,
			userAgent,
			eventType,
			instance,
			eventUuid,
			payload,
			token,
			gotAllHeaders
			)
		VALUES (?,?,?,?,?,?,?,?)`

	stmntInsertSQLGitlabEvent, err := dbHandle.Prepare(insertSQLGitlabEvent)
	if err != nil {
		log.Fatalln(err.Error())
	}

	// Open Log file
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	checkErr(err)

	// Send logs to STDOUT and Log file
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)
	log.Print("Started the GitLab webhook listner.\n")

	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		var bodyBytes []byte
		var vgitlabEvent gitlabEvent

		//log.Print("Inside handler\n")

		//requestDump, err := httputil.DumpRequest(r, true)
		//if err != nil {
		//	fmt.Println(err)
		//}
		//fmt.Println(string(requestDump))
		//fmt.Printf("%s", requestDump)

		//fmt.Printf("Headers: %+v\n", r.Header)
		headers := make([]string, len(r.Header))

		//  i := 0
		//  for header := range r.Header {
		//  	headers[i] = header
		//  	i++
		//  	//fmt.Printf("%s: %s\n", header, r.Header[header][0])
		//  }
		gitlabHeaders := []string{
			"X-Gitlab-Instance",
			"X-Gitlab-Event",
			"X-Gitlab-Token",
			"X-Gitlab-Event-Uuid",
		}
		if containsValuesInArray(gitlabHeaders, headers) {
			//fmt.Println("Array contains one of the specified values")
			vgitlabEvent = gitlabEvent{
				contentLength: r.Header["Content-Length"][0],
				eventType:     r.Header["X-Gitlab-Event"][0],
				instance:      r.Header["X-Gitlab-Instance"][0],
				token:         r.Header["X-Gitlab-Token"][0],
				userAgent:     r.Header["User-Agent"][0],
				eventUuid:     r.Header["X-Gitlab-Event-Uuid"][0],
			}
			// fmt.Printf("%s\n", vgitlabEvent.contentLength)
		} else {
			fmt.Println("Array does not contain any of the specified values")
		}

		if r.Body != nil {
			bodyBytes, err = ioutil.ReadAll(r.Body)
			if err != nil {
				fmt.Printf("Body reading error: %v", err)
				return
			}
			defer r.Body.Close()
		}
		if len(bodyBytes) > 0 {
			//var prettyJSON bytes.Buffer
			//if err = json.Indent(&prettyJSON, bodyBytes, "", "\t"); err != nil {
			//	fmt.Printf("JSON parse error: %v", err)
			//	return
			//}
			//fmt.Println(string(prettyJSON.Bytes()))
			if bodyJSON, err := json.Marshal(bodyBytes); err != nil {
				fmt.Printf("JSON parse error: %v", err)
				return
			} else {
				//fmt.Printf("%+s", bodyJSON)
				str1 := string(bodyJSON[1 : len(bodyJSON)-1]) // body is double qouted
				//fmt.Println(str1)
				x, _ := b64.StdEncoding.DecodeString(str1)
				//fmt.Printf("%s\n", string(x))
				vgitlabEvent.payload = string(x)
			}
			_, err = stmntInsertSQLGitlabEvent.Exec(
				vgitlabEvent.contentLength,
				vgitlabEvent.userAgent,
				vgitlabEvent.eventType,
				vgitlabEvent.instance,
				vgitlabEvent.eventUuid,
				vgitlabEvent.payload,
				vgitlabEvent.token,
				vgitlabEvent.gotAllHeaders,
			)
			if err != nil {
				log.Fatalln(err.Error())
			}
		} else {
			fmt.Printf("Body: No Body Supplied\n")
		}
		//fmt.Printf("%+v\n", vgitlabEvent)
	})
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe("0.0.0.0:4000", nil)
}
func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

// BUILD
// =====
// object_kind
// build_id
// build_name
// build_stage
// build_status
// build_created_at
// build_started_at
// build_finished_at
// build_duration
// build_queued_duration
// build_failure_reason
// pipeline_id
// runner.id
// runner.description
// runner.runner_type
// runner.is_shared
// runner.tags []
// project_id
// project_name
//
//
// PIPELINE
// ========
// object_kind
// object_attributes.id
// object_attributes.source
// object_attributes.status
// object_attributes.detailed_status
// object_attributes.stages []
// created_at
// finished_at
// duration
// queued_duration
// project.id
// project.path_with_namespace

// type build struct {
// 	ObjectKind          string      `json:"object_kind"`
// 	Ref                 string      `json:"ref"`
// 	Tag                 bool        `json:"tag"`
// 	BeforeSha           string      `json:"before_sha"`
// 	Sha                 string      `json:"sha"`
// 	BuildID             int64       `json:"build_id"`
// 	BuildName           string      `json:"build_name"`
// 	BuildStage          string      `json:"build_stage"`
// 	BuildStatus         string      `json:"build_status"`
// 	BuildCreatedAt      string      `json:"build_created_at"`
// 	BuildStartedAt      string      `json:"build_started_at"`
// 	BuildFinishedAt     string      `json:"build_finished_at"`
// 	BuildDuration       float64     `json:"build_duration"`
// 	BuildQueuedDuration float64     `json:"build_queued_duration"`
// 	BuildAllowFailure   bool        `json:"build_allow_failure"`
// 	BuildFailureReason  string      `json:"build_failure_reason"`
// 	PipelineID          int         `json:"pipeline_id"`
// 	Runner              Runner      `json:"runner"`
// 	ProjectID           int         `json:"project_id"`
// 	ProjectName         string      `json:"project_name"`
// 	User                User        `json:"user"`
// 	Commit              Commit      `json:"commit"`
// 	Repository          Repository  `json:"repository"`
// 	Environment         interface{} `json:"environment"`
// }
// type Runner struct {
// 	ID          int      `json:"id"`
// 	Description string   `json:"description"`
// 	RunnerType  string   `json:"runner_type"`
// 	Active      bool     `json:"active"`
// 	IsShared    bool     `json:"is_shared"`
// 	Tags        []string `json:"tags"`
// }
// type User struct {
// 	ID        int    `json:"id"`
// 	Name      string `json:"name"`
// 	Username  string `json:"username"`
// 	AvatarURL string `json:"avatar_url"`
// 	Email     string `json:"email"`
// }
// type Commit struct {
// 	ID          int         `json:"id"`
// 	Name        interface{} `json:"name"`
// 	Sha         string      `json:"sha"`
// 	Message     string      `json:"message"`
// 	AuthorName  string      `json:"author_name"`
// 	AuthorEmail string      `json:"author_email"`
// 	AuthorURL   string      `json:"author_url"`
// 	Status      string      `json:"status"`
// 	Duration    int         `json:"duration"`
// 	StartedAt   string      `json:"started_at"`
// 	FinishedAt  string      `json:"finished_at"`
// }
// type Repository struct {
// 	Name            string      `json:"name"`
// 	URL             string      `json:"url"`
// 	Description     interface{} `json:"description"`
// 	Homepage        string      `json:"homepage"`
// 	GitHTTPURL      string      `json:"git_http_url"`
// 	GitSSHURL       string      `json:"git_ssh_url"`
// 	VisibilityLevel int         `json:"visibility_level"`
// }

//  build {Runner, User, Commit, Repository}
//  pipeline {ObjectAttributes {variables}, User, Project, Commit{Author}, Builds{ArtifactsFile,User,Runner}, SourcePipeline{Project} }

// type pipeline struct {
// 	ObjectKind       string           `json:"object_kind"`
// 	ObjectAttributes ObjectAttributes `json:"object_attributes"`
// 	MergeRequest     interface{}      `json:"merge_request"`
// 	User             User             `json:"user"`
// 	Project          Project          `json:"project"`
// 	Commit           Commit           `json:"commit"`
// 	Builds           []Builds         `json:"builds"`
// 	SourcePipeline   SourcePipeline   `json:"source_pipeline"`
// }
// type Variables struct {
// 	Key   string `json:"key"`
// 	Value string `json:"value"`
// }
// type ObjectAttributes struct {
// 	ID             int         `json:"id"`
// 	Iid            int         `json:"iid"`
// 	Ref            string      `json:"ref"`
// 	Tag            bool        `json:"tag"`
// 	Sha            string      `json:"sha"`
// 	BeforeSha      string      `json:"before_sha"`
// 	Source         string      `json:"source"`
// 	Status         string      `json:"status"`
// 	DetailedStatus string      `json:"detailed_status"`
// 	Stages         []string    `json:"stages"`
// 	CreatedAt      string      `json:"created_at"`
// 	FinishedAt     string      `json:"finished_at"`
// 	Duration       int         `json:"duration"`
// 	QueuedDuration int         `json:"queued_duration"`
// 	Variables      []Variables `json:"variables"`
// }
// type User struct {
// 	ID        int    `json:"id"`
// 	Name      string `json:"name"`
// 	Username  string `json:"username"`
// 	AvatarURL string `json:"avatar_url"`
// 	Email     string `json:"email"`
// }
// type Project struct {
// 	ID                int         `json:"id"`
// 	Name              string      `json:"name"`
// 	Description       interface{} `json:"description"`
// 	WebURL            string      `json:"web_url"`
// 	AvatarURL         interface{} `json:"avatar_url"`
// 	GitSSHURL         string      `json:"git_ssh_url"`
// 	GitHTTPURL        string      `json:"git_http_url"`
// 	Namespace         string      `json:"namespace"`
// 	VisibilityLevel   int         `json:"visibility_level"`
// 	PathWithNamespace string      `json:"path_with_namespace"`
// 	DefaultBranch     string      `json:"default_branch"`
// 	CiConfigPath      string      `json:"ci_config_path"`
// }
// type Author struct {
// 	Name  string `json:"name"`
// 	Email string `json:"email"`
// }
// type Commit struct {
// 	ID        string    `json:"id"`
// 	Message   string    `json:"message"`
// 	Title     string    `json:"title"`
// 	Timestamp time.Time `json:"timestamp"`
// 	URL       string    `json:"url"`
// 	Author    Author    `json:"author"`
// }
// type Runner struct {
// 	ID          int      `json:"id"`
// 	Description string   `json:"description"`
// 	RunnerType  string   `json:"runner_type"`
// 	Active      bool     `json:"active"`
// 	IsShared    bool     `json:"is_shared"`
// 	Tags        []string `json:"tags"`
// }
// type ArtifactsFile struct {
// 	Filename interface{} `json:"filename"`
// 	Size     interface{} `json:"size"`
// }
// type Builds struct {
// 	ID             int64         `json:"id"`
// 	Stage          string        `json:"stage"`
// 	Name           string        `json:"name"`
// 	Status         string        `json:"status"`
// 	CreatedAt      string        `json:"created_at"`
// 	StartedAt      string        `json:"started_at"`
// 	FinishedAt     string        `json:"finished_at"`
// 	Duration       float64       `json:"duration"`
// 	QueuedDuration float64       `json:"queued_duration"`
// 	FailureReason  interface{}   `json:"failure_reason"`
// 	When           string        `json:"when"`
// 	Manual         bool          `json:"manual"`
// 	AllowFailure   bool          `json:"allow_failure"`
// 	User           User          `json:"user"`
// 	Runner         Runner        `json:"runner"`
// 	ArtifactsFile  ArtifactsFile `json:"artifacts_file"`
// 	Environment    interface{}   `json:"environment"`
// }
// type Project struct {
// 	ID                int    `json:"id"`
// 	WebURL            string `json:"web_url"`
// 	PathWithNamespace string `json:"path_with_namespace"`
// }
// type SourcePipeline struct {
// 	Project    Project `json:"project"`
// 	JobID      int64   `json:"job_id"`
// 	PipelineID int     `json:"pipeline_id"`
// }
