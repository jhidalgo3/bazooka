package main

import (
	"log"
	"os"
	"time"

	"fmt"

	lib "github.com/bazooka-ci/bazooka/commons"
	"github.com/bazooka-ci/bazooka/commons/mongo"
)

const (
	BazookaEnvSCMKeyfile = "BZK_SCM_KEYFILE"
	BazookaEnvHome       = "BZK_HOME"
	BazookaEnvDockerSock = "BZK_DOCKERSOCK"
	BazookaEnvApiUrl     = "BZK_API_URL"
	BazookaEnvSyslogUrl  = "BZK_SYSLOG_URL"
	BazookaEnvMongoAddr  = "MONGO_PORT_27017_TCP_ADDR"
	BazookaEnvMongoPort  = "MONGO_PORT_27017_TCP_PORT"

	DockerSock     = "/var/run/docker.sock"
	DockerEndpoint = "unix://" + DockerSock
	BazookaHome    = "/bazooka"
)

type context struct {
	apiUrl    string
	syslogUrl string
	mongoAddr string
	mongoPort string
	connector *mongo.MongoConnector
	paths     paths
}

type paths struct {
	home           path
	scmKey         path
	cryptoKey      path
	dockerSock     path
	dockerEndpoint path
}

type path struct {
	container string
	host      string
}

func initContext() *context {
	c := &context{
		apiUrl:    os.Getenv(BazookaEnvApiUrl),
		syslogUrl: os.Getenv(BazookaEnvSyslogUrl),
		mongoAddr: os.Getenv(BazookaEnvMongoAddr),
		mongoPort: os.Getenv(BazookaEnvMongoPort),
		paths: paths{
			home:           path{BazookaHome, os.Getenv(BazookaEnvHome)},
			scmKey:         path{"", os.Getenv(BazookaEnvSCMKeyfile)},
			dockerSock:     path{DockerSock, os.Getenv(BazookaEnvDockerSock)},
			dockerEndpoint: path{DockerEndpoint, "unix://" + os.Getenv(BazookaEnvDockerSock)},
		},
	}

	if err := lib.WaitForTcpConnection(c.mongoAddr, c.mongoPort, 100*time.Millisecond, 5*time.Second); err != nil {
		log.Fatalf("Cannot connect to the database: %v", err)
	}
	c.connector = mongo.NewConnector()

	fmt.Printf("server init, context=%#v\n", c)
	return c
}

func (c *context) cleanup() {
	c.connector.Close()
}
