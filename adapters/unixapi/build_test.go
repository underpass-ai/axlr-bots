package unixapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

type fixedBuild struct{ running, installed domain.BuildIdentity }

func (f fixedBuild) Running() domain.BuildIdentity            { return f.running }
func (f fixedBuild) Installed() (domain.BuildIdentity, error) { return f.installed, nil }

func TestBuildRouteSaysWhenTheRunningBuildIsStale(t *testing.T) {
	o := serveOperatorAddressWith(t, func(deps *application.Dependencies) {
		deps.Build = fixedBuild{running: domain.BuildIdentity{Revision: "aaa", Time: "t"}, installed: domain.BuildIdentity{Revision: "bbb", Modified: true}}
		deps.MinimumAXLR = "0.20.0"
	})
	get := func(token string) (int, buildAnswer) {
		request, _ := http.NewRequest(http.MethodGet, o.url+"/v1/build", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := o.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		var answer buildAnswer
		_ = json.Unmarshal(raw, &answer)
		return response.StatusCode, answer
	}
	if code, _ := get(""); code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", code)
	}
	code, answer := get(testToken)
	want := buildAnswer{Running: buildIdentityDTO{Revision: "aaa", Time: "t"}, Installed: buildIdentityDTO{Revision: "bbb", Modified: true}, Stale: true, MinimumAXLR: "0.20.0"}
	if code != http.StatusOK || answer != want {
		t.Fatalf("got %d %+v, want %+v", code, answer, want)
	}
}

func TestBuildRouteLeavesOutAMinimumNobodySet(t *testing.T) {
	o := serveOperatorAddress(t)
	request, _ := http.NewRequest(http.MethodGet, o.url+"/v1/build", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := o.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || strings.Contains(string(raw), "minimumAxlr") {
		t.Fatalf("%d %s", response.StatusCode, raw)
	}
}
