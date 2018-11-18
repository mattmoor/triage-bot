package github

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/google/go-github/github"
)

// formatRequest generates ascii representation of a request
// from: https://medium.com/doing-things-right/pretty-printing-http-requests-in-golang-a918d5aaa000
func formatRequest(r *http.Request) string {
	// Create return string
	var request []string
	// Add the request string
	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, url)
	// Add the host
	request = append(request, fmt.Sprintf("Host: %v", r.Host))
	// Loop through headers
	for name, headers := range r.Header {
		name = strings.ToLower(name)
		for _, h := range headers {
			request = append(request, fmt.Sprintf("%v: %v", name, h))
		}
	}

	// If this is a POST, add post data
	if r.Method == "POST" {
		r.ParseForm()
		request = append(request, "\n")
		request = append(request, r.Form.Encode())
	}
	// Return the request as a string
	return strings.Join(request, "\n")
}

func Handler(w http.ResponseWriter, r *http.Request) {
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("ERROR: no payload: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// TODO(mattmoor): This should be:
	//     eventType := github.WebHookType(r)
	// https://github.com/knative/eventing-sources/issues/120
	// HACK HACK HACK
	eventType := strings.Split(r.Header.Get("ce-eventtype"), ".")[4]

	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		log.Printf("ERROR: unable to parse webhook: %v\n%v", err, formatRequest(r))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	handleErr := func(event interface{}, err error) {
		if err == nil {
			fmt.Fprintf(w, "Handled %T", event)
			return
		}
		log.Printf("Error handling %T: %v", event, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The set of events here should line up with what is in
	//   config/one-time/github-source.yaml
	switch event := event.(type) {
	case *github.PullRequestEvent:
		handleErr(event, HandlePullRequest(event))
	case *github.PushEvent:
		handleErr(event, HandlePush(event))
	case *github.PullRequestReviewEvent:
		handleErr(event, HandleOther(event))
	case *github.PullRequestReviewCommentEvent:
		handleErr(event, HandleOther(event))
	case *github.IssuesEvent:
		handleErr(event, HandleIssues(event))
	case *github.IssueCommentEvent:
		handleErr(event, HandleIssueComment(event))
	default:
		log.Printf("Unrecognized event: %T", event)
		http.Error(w, "Unknown event", http.StatusBadRequest)
		return
	}
}

func HandleIssueComment(ice *github.IssueCommentEvent) error {
	log.Printf("Comment from %s on #%d: %q",
		ice.Sender.GetLogin(),
		ice.Issue.GetNumber(),
		ice.Comment.GetBody())

	// TODO(mattmoor): Is ice.Repo.Owner.Login reliable for organizations, or do we
	// have to parse the FullName?
	//    Owner: mattmoor, Repo: kontext, Fullname: mattmoor/kontext
	// log.Printf("Owner: %s, Repo: %s, Fullname: %s", *ice.Repo.Owner.Login, *ice.Repo.Name,
	// 	*ice.Repo.FullName)

	if strings.Contains(*ice.Comment.Body, "Hello there.") {
		ctx := context.Background()
		ghc := GetClient(ctx)

		msg := fmt.Sprintf("Hello @%s", ice.Sender.GetLogin())

		_, _, err := ghc.Issues.CreateComment(ctx,
			ice.Repo.Owner.GetLogin(), ice.Repo.GetName(), ice.Issue.GetNumber(),
			&github.IssueComment{
				Body: &msg,
			})
		return err
	}

	return nil
}

func HandleIssues(ie *github.IssuesEvent) error {
	log.Printf("Issue: %v", ie.GetIssue().String())

	// See https://developer.github.com/v3/activity/events/types/#issuesevent

	ctx := context.Background()
	ghc := GetClient(ctx)

	msg := fmt.Sprintf("Issue event: %v", ie.GetAction())
	_, _, err := ghc.Issues.CreateComment(ctx,
		ie.Repo.Owner.GetLogin(), ie.Repo.GetName(), ie.GetIssue().GetNumber(),
		&github.IssueComment{
			Body: &msg,
		})
	return err
}

func HandlePullRequest(pre *github.PullRequestEvent) error {
	log.Printf("PR: %v", pre.GetPullRequest().String())

	// TODO(mattmoor): To respond to code changes, I think the appropriate set of events are:
	// 1. opened
	// 2. reopened
	// 3. synchronized

	// (from https://developer.github.com/v3/activity/events/types/#pullrequestevent)
	// Other events we might see include:
	// * assigned
	// * unassigned
	// * review_requested
	// * review_request_removed
	// * labeled
	// * unlabeled
	// * edited
	// * closed

	ctx := context.Background()
	ghc := GetClient(ctx)

	msg := fmt.Sprintf("PR event: %v", pre.GetAction())
	_, _, err := ghc.Issues.CreateComment(ctx,
		pre.Repo.Owner.GetLogin(), pre.Repo.GetName(), pre.GetNumber(),
		&github.IssueComment{
			Body: &msg,
		})
	return err
}

func HandlePush(pe *github.PushEvent) error {
	log.Printf("Push: %v", pe.String())
	return nil
}

func HandleOther(event interface{}) error {
	log.Printf("TODO %T: %#v", event, event)
	return nil
}
