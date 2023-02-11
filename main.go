package main

import (
	"database/sql"
	// b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	_ "os"

	"github.com/go-playground/webhooks/v6/gitlab"
	_ "github.com/mattn/go-sqlite3"
)

const (
	path                = "/webhooks"
	defaultDBFile       = "/tmp/webbook.db"
	defaultLogFile      = "/tmp/webhook-log.txt"
	defaultGitlabSecret = "MyGitLabToken"
	//defaultGithubSecret = "MyGitHubToken"
)

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

	eventTableSQL := `CREATE TABLE IF NOT EXISTS k8Event(
			"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
			created_at integer(4) not null default (strftime('%s','now')),
			processed_at integer(4),
			"event" TEXT
		);`

	podResourceTableSQL := `CREATE TABLE IF NOT EXISTS k8PodResource(
			"id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
			created_at integer(4) not null default (strftime('%s','now')),
			processed_at integer(4),
			"pod_resource" TEXT
		);`

	statement, err := dbConn.Prepare(eventTableSQL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement.Exec() // Execute SQL Statements

	statement1, err := dbConn.Prepare(podResourceTableSQL) // Prepare SQL Statement
	if err != nil {
		log.Fatal(err.Error())
	}
	statement1.Exec() // Execute SQL Statements
	return dbConn
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
	insertSQL := "INSERT INTO k8Event(event) VALUES (?)"
	statement, err := dbHandle.Prepare(insertSQL)
	if err != nil {
		log.Fatalln(err.Error())
	}
	_, err = statement.Exec("Hello")
	if err != nil {
		log.Fatalln(err.Error())
	}

	//db, err := sql.Open("sqlite3", dbFileName)
	//checkErr(err)

	// Open Log file
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
	checkErr(err)

	// Send logs to STDOUT and Log file
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)
	log.Print("Started the GitLab webhook listner.\n")

	// Setup GitLab Token in Library
	hook, _ := gitlab.New(gitlab.Options.Secret(gitlabSecret))

	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		//var bodyBytes []byte

		//log.Print("Inside handler\n")

		//requestDump, err := httputil.DumpRequest(r, true)
		//if err != nil {
		//	fmt.Println(err)
		//}
		//fmt.Println(string(requestDump))
		//fmt.Printf("%s", requestDump)

		//fmt.Printf("Headers: %+v\n", r.Header)
		headers := make([]string, len(r.Header))

		i := 0
		for header := range r.Header {
			headers[i] = header
			i++
			fmt.Printf("%s: %s\n", header, r.Header[header][0])
		}
		// fmt.Printf("Headers: %s\n", r.Header["Content-Type"][0])
		// fmt.Printf("Headers: %s\n", r.Header["User-Agent"][0])
		// fmt.Printf("Headers: %s\n", r.Header["X-Gitlab-Event"][0])
		// fmt.Printf("Headers: %s\n", r.Header["X-Gitlab-Instance"][0])
		// fmt.Printf("Headers: %s\n", r.Header["X-Gitlab-Token"][0])
		// fmt.Printf("Headers: %s\n", r.Header["X-Gitlab-Event-Uuid"][0])

		// if r.Body != nil {
		// 	bodyBytes, err = ioutil.ReadAll(r.Body)
		// 	if err != nil {
		// 		fmt.Printf("Body reading error: %v", err)
		// 		return
		// 	}
		// 	defer r.Body.Close()
		// }
		// if len(bodyBytes) > 0 {
		// 	//var prettyJSON bytes.Buffer
		// 	//if err = json.Indent(&prettyJSON, bodyBytes, "", "\t"); err != nil {
		// 	//	fmt.Printf("JSON parse error: %v", err)
		// 	//	return
		// 	//}
		// 	//fmt.Println(string(prettyJSON.Bytes()))
		// 	if bodyJSON, err := json.Marshal(bodyBytes); err != nil {
		// 		fmt.Printf("JSON parse error: %v", err)
		// 		return
		// 	} else {
		// 		//fmt.Printf("%+s", bodyJSON)
		// 		str1 := string(bodyJSON[1 : len(bodyJSON)-1]) // body is double qouted
		// 		//fmt.Println(str1)
		// 		x, _ := b64.StdEncoding.DecodeString(str1)
		// 		fmt.Printf("%s", string(x))
		// 	}
		// } else {
		// 	fmt.Printf("Body: No Body Supplied\n")
		// }

		//https://pkg.go.dev/github.com/go-playground/webhooks/v6@v6.0.0-beta.3/gitlab#Event
		payload, err := hook.Parse(r,
			gitlab.PushEvents,
			gitlab.TagEvents,
			gitlab.IssuesEvents,
			gitlab.ConfidentialIssuesEvents,
			gitlab.CommentEvents,
			gitlab.MergeRequestEvents,
			gitlab.WikiPageEvents,
			gitlab.PipelineEvents,
			gitlab.BuildEvents,
			gitlab.JobEvents,
			gitlab.SystemHookEvents)
		if err != nil {
			if err == gitlab.ErrEventNotFound {
				// ok event wasn;t one of the ones asked to be parsed
			}
		}

		//log.Printf("%+v", payload)
		switch payload.(type) {

		// ConfidentialIssueEvent
		case gitlab.ConfidentialIssueEventPayload:
			confidentialIssue := payload.(gitlab.ConfidentialIssueEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", confidentialIssue)
			confidentialIssueJsonData, err := json.Marshal(confidentialIssue)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", confidentialIssueJsonData)

		// PushEvent
		case gitlab.PushEventPayload:
			push := payload.(gitlab.PushEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", push)
			pushJsonData, err := json.Marshal(push)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", pushJsonData)

		// TagEvent
		case gitlab.TagEventPayload:
			tag := payload.(gitlab.TagEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", tag)
			tagJsonData, err := json.Marshal(tag)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", tagJsonData)

		// IssueEvent
		case gitlab.IssueEventPayload:
			issue := payload.(gitlab.IssueEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", issues)
			issueJsonData, err := json.Marshal(issue)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", issueJsonData)

		// CommentEvent
		case gitlab.CommentEventPayload:
			comment := payload.(gitlab.CommentEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", comment)
			commentJsonData, err := json.Marshal(comment)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", commentJsonData)

		// MergeRequestEvent
		case gitlab.MergeRequestEventPayload:
			mergeRequest := payload.(gitlab.MergeRequestEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", mergeRequest)
			mergeRequestJsonData, err := json.Marshal(mergeRequest)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", mergeRequestJsonData)

		// WikiPageEvent
		case gitlab.WikiPageEventPayload:
			wikiPage := payload.(gitlab.WikiPageEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", wikiPage)
			wikiPageJsonData, err := json.Marshal(wikiPage)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", wikiPageJsonData)

		// PipelineEvent
		case gitlab.PipelineEventPayload:
			pipeline := payload.(gitlab.PipelineEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", pipeline)
			//pipelineJsonData, err := json.Marshal(pipeline)
			//if err != nil {
			//	log.Fatal(err)
			//}
			//log.Printf("%s\n", pipelineJsonData)
			pipeline_id := pipeline.ObjectAttributes.ID
			pipeline_status := pipeline.ObjectAttributes.Status
			pipeline_duration := pipeline.ObjectAttributes.Duration
			pipeline_project_name := pipeline.Project.PathWithNamespace
			//for i, build := range pipeline.Builds {
			for _, build := range pipeline.Builds {
				//log.Printf("\npipeline id: %d, build id: %d, runer id: %d, status %s Stage %s Name %s build index%d  %+v",
				log.Printf("\n{\"pipeline_id\": \"%d\", \"Status\": \"%s\", \"Project\":\"%s\", \"duration\": \"%d\", \"build\": {\"id\": \"%d\", \"status\": \"%s\"}, \"runner\": {\"id\": \"%d\",  \"desc\": \"%s\"}}",
					pipeline_id,
					pipeline_status,
					pipeline_project_name,
					pipeline_duration,
					build.ID,
					build.Status,
					build.Runner.ID,
					build.Runner.Description)
				//	build.Stage,
				//	build.Name,
				//	i, build)
			}

		// BuildEvent
		case gitlab.BuildEventPayload:
			build := payload.(gitlab.BuildEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", build)
			//buildJsonData, err := json.Marshal(build)
			//if err != nil {
			//	log.Fatal(err)
			//}
			//log.Printf("%s\n", buildJsonData)
			log.Printf("\n{\"build_id\": \"%d\", \"Status\": \"%s\", \"stage\": \"%s\", \"name\": \"%s\", \"started_at\": \"%s\", \"finished_at\": \"%s\", \"duration\": \"%0.2f\"}",
				build.BuildID,
				build.BuildStatus,
				build.BuildStage,
				build.BuildName,
				build.BuildStartedAt.Time,
				build.BuildFinishedAt.Time,
				build.BuildDuration)
			//build)

		// JobEvent
		case gitlab.JobEventPayload:
			job := payload.(gitlab.JobEventPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", job)
			jobJsonData, err := json.Marshal(job)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", jobJsonData)

		// SystemHookEvent
		case gitlab.SystemHookPayload:
			systemHook := payload.(gitlab.SystemHookPayload)
			// Do whatever you want from here...
			//log.Printf("%+v", systemHook)
			systemHookJsonData, err := json.Marshal(systemHook)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("%s\n", systemHookJsonData)
			//case gitlab.JobEventPayload:
			//	//https://docs.gitlab.com/ee/user/project/integrations/webhooks.html#job-events
			//	job := payload.(gitlab.JobEventPayload)
			//	// Do whatever you want from here...
			//	//log.Printf("%+v", job)
			//	data, err := json.Marshal(job)
			//	if err != nil {
			//		log.Fatal(err)
			//	}
			//	log.Printf("%s\n", data)

			//case gitlab.BuildEventPayload:
			//	//
			//	build := payload.(gitlab.BuildEventPayload)
			//	// Do whatever you want from here...
			//	//log.Printf("%+v", build)
			//	data, err := json.Marshal(build)
			//	if err != nil {
			//		log.Fatal(err)
			//	}
			//	log.Printf("%s\n", data)
		}
	})
	http.ListenAndServe(":4000", nil)
}
func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
