package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"

	log "github.com/Sirupsen/logrus"
	"github.com/haklop/bazooka/commons/matrix"

	bazooka "github.com/haklop/bazooka/commons"
)

const (
	SourceFolder = "/bazooka"
	OutputFolder = "/bazooka-output"
	MetaFolder   = "/meta"
	Golang       = "go"
)

func main() {
	file, err := bazooka.ResolveConfigFile(SourceFolder)
	if err != nil {
		log.Fatal(err)
	}

	conf := &ConfigGolang{}
	err = bazooka.Parse(file, conf)
	if err != nil {
		log.Fatal(err)
	}

	mx := matrix.Matrix{
		Golang: conf.GoVersions,
	}

	if len(conf.GoVersions) == 0 {
		mx[Golang] = []string{"tip"}
	}
	mx.IterAll(func(permutation map[string]string, counter string) {
		if err := manageGoVersion(counter, conf, permutation[Golang]); err != nil {
			log.Fatal(err)
		}
	}, nil)
}

func manageGoVersion(counter string, conf *ConfigGolang, version string) error {
	conf.GoVersions = []string{}
	setGodir(conf)
	setDefaultInstall(conf)
	err := setDefaultScript(conf)
	if err != nil {
		return err
	}
	image, err := resolveGoImage(version)
	conf.Base.FromImage = image
	if err != nil {
		return err
	}

	err = bazooka.AppendToFile(fmt.Sprintf("%s/%s", MetaFolder, counter), fmt.Sprintf("%s: %s\n", Golang, version), 0644)
	if err != nil {
		return err
	}
	return bazooka.Flush(conf, fmt.Sprintf("%s/.bazooka.%s.yml", OutputFolder, counter))
}

func setGodir(conf *ConfigGolang) {
	env := bazooka.GetEnvMap(conf.Base.Env)

	godirExist, err := bazooka.FileExists("/bazooka/.godir")
	if err != nil {
		log.Fatal(err)
	}

	var buildDir string
	if godirExist {
		f, err := os.Open("/bazooka/.godir")
		defer f.Close()
		if err != nil {
			log.Fatal(err)
		}

		bf := bufio.NewReader(f)

		// only read first line
		content, isPrefix, err := bf.ReadLine()

		if err == io.EOF {
			buildDir = "/go/src/app"
		} else if err != nil {
			log.Fatal(err)
		} else if isPrefix {
			log.Fatal("Unexpected long line reading", f.Name())
		} else {
			buildDir = fmt.Sprintf("/go/src/%s", content)
		}

	} else {
		scmMetadata := &bazooka.SCMMetadata{}
		scmMetadataFile := fmt.Sprintf("%s/scm", MetaFolder)
		bazooka.Parse(scmMetadataFile, scmMetadata)

		if len(scmMetadata.Origin) > 0 {
			r, err := regexp.Compile("^(?:https://(?:\\w+@){0,1}|git@)(github.com|bitbucket.org)[:/]{0,1}([\\w-_]+/[\\w-_]+).git$")
			if err != nil {
				log.Fatal(err)
			}

			res := r.FindStringSubmatch(scmMetadata.Origin)
			if res != nil {
				buildDir = fmt.Sprintf("/go/src/%s/%s", res[1], res[2])
			} else {
				buildDir = "/go/src/app"
			}
		} else {
			buildDir = "/go/src/app"
		}
	}

	log.Info("Buildir set to %s\n", buildDir)

	env["BZK_BUILD_DIR"] = []string{buildDir}

	conf.Base.Env = flattenEnvMap(env)
}

func setDefaultInstall(conf *ConfigGolang) {
	if len(conf.Base.Install) == 0 {
		conf.Base.Install = []string{"go get -d -t -v ./... && go build -v ./..."}
	}
}

func setDefaultScript(conf *ConfigGolang) error {
	if len(conf.Base.Script) == 0 {
		if _, err := os.Open(fmt.Sprintf("%s/Makefile", SourceFolder)); err != nil {
			if os.IsNotExist(err) {
				conf.Base.Script = []string{"go test -v ./..."}
				return nil
			}
			return err
		}
		conf.Base.Script = []string{"make"}
	}
	return nil
}

func resolveGoImage(version string) (string, error) {
	//TODO extract this from db
	goMap := map[string]string{
		"1.2.2":  "bazooka/runner-golang:1.2.2",
		"1.3":    "bazooka/runner-golang:1.3",
		"1.3.1":  "bazooka/runner-golang:1.3.1",
		"1.3.2":  "bazooka/runner-golang:1.3.2",
		"1.3.3":  "bazooka/runner-golang:1.3.3",
		"tip":    "bazooka/runner-golang:latest",
		"latest": "bazooka/runner-golang:latest",
	}
	if val, ok := goMap[version]; ok {
		return val, nil
	}
	return "", fmt.Errorf("Unable to find Bazooka Docker Image for Go Runnner %s\n", version)
}

func flattenEnvMap(mapp map[string][]string) []string {
	res := []string{}
	for key, values := range mapp {
		for _, value := range values {
			res = append(res, fmt.Sprintf("%s=%s", key, value))
		}
	}
	return res
}
