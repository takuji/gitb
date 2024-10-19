package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/errors"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport"
)

type Repository interface {
	HeadName() string
	HeadShortName() string
	RemoteEndpointHost() string
	RemoteEndpointPath() string
	RootDirectory() string
	LsRemote() (RefToHash, error)
}

func OpenRepository(path string) (Repository, error) {
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return nil, err
	}
	cfg := remote.Config()
	if len(cfg.URLs) == 0 {
		return nil, errors.New("could not find remote URL")
	}
	u := cfg.URLs[0]
	ep, err := transport.NewEndpoint(u)
	if err != nil {
		return nil, err
	}
	return &repository{
		repo: repo,
		head: head,
		ep:   ep,
	}, nil
}

type repository struct {
	repo *git.Repository
	head *plumbing.Reference
	ep   *transport.Endpoint
}

func (r repository) HeadName() string {
	return r.head.Name().String()
}

func (r repository) HeadShortName() string {
	return r.head.Name().Short()
}

func (r repository) RemoteEndpointHost() string {
	return r.ep.Host
}

func (r repository) RemoteEndpointPath() string {
	return r.ep.Path
}

func (r repository) RootDirectory() string {
	wt, err := r.repo.Worktree()
	if err != nil {
		panic(err)
	}
	return wt.Filesystem.Root()
}

func (r repository) LsRemote() (RefToHash, error) {
	cmd := exec.Command("git", "ls-remote", "-q")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return toRefToHash(out), nil
}

func toRefToHash(b []byte) RefToHash {
	refToHash := make(RefToHash)
	remotes := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	for _, v := range remotes {
		delimited := strings.Split(v, "\t")
		hash := delimited[0]
		ref := delimited[1]
		refToHash[ref] = hash
	}
	return refToHash
}

func NewBacklogRepository(repo Repository) *BacklogRepository {
	spaceKey, domain := extractSpaceKeyAndDomain(repo.RemoteEndpointHost())
	projectKey, repoName := extractProjectKeyAndRepoName(repo.RemoteEndpointPath())
	return &BacklogRepository{
		openBrowser: openBrowser,
		repo:        repo,
		domain:      domain,
		spaceKey:    spaceKey,
		projectKey:  projectKey,
		repoName:    repoName,
	}
}

func extractSpaceKeyAndDomain(host string) (spaceKey, domain string) {
	delimitedHost := strings.Split(host, ".")
	spaceKey = delimitedHost[0]
	domain = strings.Join(delimitedHost[len(delimitedHost)-2:], ".")
	return
}

func extractProjectKeyAndRepoName(path string) (projectKey, repoName string) {
	epPath := strings.TrimPrefix(path, "/git")
	delimitedPath := strings.Split(epPath, "/")
	projectKey = delimitedPath[1]
	repoName = strings.TrimSuffix(delimitedPath[2], ".git")
	return
}

type BacklogRepository struct {
	openBrowser func(url string) error
	repo        Repository
	domain      string
	spaceKey    string
	projectKey  string
	repoName    string
}

func (b *BacklogRepository) OpenObject(absPath string, isDirectory bool, line string) error {
	root := b.repo.RootDirectory()
	if !strings.HasPrefix(absPath, root) {
		return errors.New("path " + absPath + " is out of repository " + root)
	}

	if line != "" {
		if isDirectory {
			return errors.New("line cannot be set for directory.")
		} else {
			re := regexp.MustCompile("^\\d+(-\\d+)?$")
			if !re.MatchString(line) {
				return errors.New("line can be number or 'from-to' format. :" + line)
			}
		}
	}
	relPath := strings.TrimPrefix(absPath[len(root):], "/")
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		ObjectURL(b.repo.HeadShortName(), relPath, isDirectory, line))
}

func (b *BacklogRepository) OpenRepositoryList() error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		GitBaseURL())
}

func (b *BacklogRepository) OpenTree(refOrHash string) error {
	if refOrHash == "" {
		refOrHash = b.repo.HeadShortName()
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		TreeURL(refOrHash))
}

func (b *BacklogRepository) OpenHistory(refOrHash string) error {
	if refOrHash == "" {
		refOrHash = b.repo.HeadShortName()
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		HistoryURL(refOrHash))
}

func (b *BacklogRepository) OpenCommit(hash string) error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		CommitURL(hash))
}

func (b *BacklogRepository) OpenNetwork(refOrHash string) error {
	if refOrHash == "" {
		refOrHash = b.repo.HeadShortName()
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		NetworkURL(refOrHash))
}

func (b *BacklogRepository) OpenBranchList() error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		BranchListURL())
}

func (b *BacklogRepository) OpenTagList() error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		TagListURL())
}

func (b *BacklogRepository) OpenPullRequestList(status string) error {
	s, err := PRStatusFromString(status)
	if err != nil {
		return err
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		PullRequestListURL(s.Int()))
}

type PRStatus int

const (
	PRStatusAll PRStatus = iota
	PRStatusOpen
	PRStatusClosed
	PRStatusMerged
)

func (p PRStatus) Int() int {
	return int(p)
}

func PRStatusFromString(s string) (status PRStatus, err error) {
	strToStatus := make(map[string]PRStatus)
	strToStatus["all"] = PRStatusAll
	strToStatus["open"] = PRStatusOpen
	strToStatus["closed"] = PRStatusClosed
	strToStatus["merged"] = PRStatusMerged
	v, ok := strToStatus[s]
	if !ok {
		var specs []string
		for s := range strToStatus {
			specs = append(specs, s)
		}
		err = errors.Errorf("invalid pull request's. choose from %v", specs)
	}
	status = v
	return
}

func (b *BacklogRepository) OpenPullRequestByID(id string) error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		PullRequestURL(id))
}

func (b *BacklogRepository) OpenPullRequest() error {
	id, err := b.findPullRequestIDFromRemote(b.repo.HeadName())
	if err != nil {
		return err
	}
	return b.OpenPullRequestByID(id)
}

const (
	refPrefix            = "refs/"
	refPullRequestPrefix = refPrefix + "pull/"
	refPullRequestSuffix = "/head"
)

type RefToHash map[string]string

func (b *BacklogRepository) findPullRequestIDFromRemote(ref string) (string, error) {

	refToHash, err := b.repo.LsRemote()
	if err != nil {
		return "", err
	}

	targetHash, ok := refToHash[ref]
	if !ok {
		return "", errors.New("not found a current branch in remote")
	}

	var prIDs []string
	for ref, hash := range refToHash {
		if !isPRRef(ref) {
			continue
		}
		if hash != targetHash {
			continue
		}
		prIDs = append(prIDs, extractPRID(ref))
	}

	if len(prIDs) == 0 {
		return "", errors.New("not found a pull request related to current branch")
	}

	sort.Sort(sort.Reverse(sort.StringSlice(prIDs)))

	return b.selectPR(prIDs)
}

type prSelector struct {
	choices  []string
	cursor   int
	selected int
	repoName string
}

func (p prSelector) Init() tea.Cmd {
	return nil
}

func (p prSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return p, tea.Quit
		case "j", "down":
			if p.cursor < len(p.choices)-1 {
				p.cursor++
			}
		case "k", "up":
			if p.cursor > 0 {
				p.cursor--
			}
		case "enter":
			p.selected = p.cursor
			return p, tea.Quit
		}
	}
	return p, nil
}

func (p prSelector) View() string {
	s := "Multiple pull requests found. Select one:\n\n"
	for i, choice := range p.choices {
		cursor := " "
		if p.cursor == i {
			cursor = ">"
		}
		s += fmt.Sprintf("%s %s #%s\n", cursor, p.repoName, choice)
	}
	s += "\nPress 'q' to quit, 'enter' to select.\n"
	return s
}

func (b *BacklogRepository) selectPR(prIDs []string) (string, error) {
	if len(prIDs) == 1 {
		return prIDs[0], nil
	}
	v := prSelector{
		choices:  prIDs,
		cursor:   0,
		selected: -1,
		repoName: fmt.Sprintf("%s/%s", b.projectKey, b.repoName),
	}
	p := tea.NewProgram(v)
	result, err := p.Run()
	if err != nil {
		return "", err
	}
	idx := result.(prSelector).selected
	if idx == -1 {
		os.Exit(0)
	}
	return prIDs[idx], nil
}

func isPRRef(ref string) bool {
	return strings.HasPrefix(ref, refPullRequestPrefix) && strings.HasSuffix(ref, refPullRequestSuffix)
}

func extractPRID(ref string) string {
	prID := strings.TrimPrefix(ref, refPullRequestPrefix)
	return strings.TrimSuffix(prID, refPullRequestSuffix)
}

func (b *BacklogRepository) OpenAddPullRequest(base, topic string) error {
	if topic == "" {
		topic = b.repo.HeadShortName()
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		AddPullRequestURL(base, topic))
}

func (b *BacklogRepository) OpenIssue() error {
	key := extractIssueKey(b.repo.HeadShortName())
	if key == "" {
		return errors.New("could not find issue key in current branch name")
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		IssueURL(key))
}

func extractIssueKey(s string) string {
	matches := regexp.MustCompile("([A-Z0-9]+(?:_[A-Z0-9]+)*-[0-9]+)").FindStringSubmatch(s)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func (b *BacklogRepository) OpenAddIssue() error {
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		AddIssueURL())
}

type IssueStatus int

const (
	IssueStatusAll IssueStatus = iota
	IssueStatusOpen
	IssueStatusInProgress
	IssueStatusResolved
	IssueStatusClosed
	IssueStatusNotClosed
)

func (p IssueStatus) Int() int {
	return int(p)
}

func IssueStatusFromString(s string) (status IssueStatus, err error) {
	strToStatus := make(map[string]IssueStatus)
	strToStatus["all"] = IssueStatusAll
	strToStatus["open"] = IssueStatusOpen
	strToStatus["in_progress"] = IssueStatusInProgress
	strToStatus["resolved"] = IssueStatusResolved
	strToStatus["closed"] = IssueStatusClosed
	strToStatus["not_closed"] = IssueStatusNotClosed
	v, ok := strToStatus[s]
	if !ok {
		var specs []string
		for s := range strToStatus {
			specs = append(specs, s)
		}
		err = errors.Errorf("invalid issue's status. choose from %v", specs)
	}
	status = v
	return
}

func (b *BacklogRepository) OpenIssueList(state string) error {
	s, err := IssueStatusFromString(state)
	if err != nil {
		return err
	}
	var statusIds []int
	switch s {
	case IssueStatusAll:
		// Don't specify the issue status
	case IssueStatusNotClosed:
		statusIds = append(statusIds, IssueStatusOpen.Int())
		statusIds = append(statusIds, IssueStatusInProgress.Int())
		statusIds = append(statusIds, IssueStatusResolved.Int())
	default:
		statusIds = append(statusIds, s.Int())
	}
	return b.openBrowser(NewBacklogURLBuilder(b.domain, b.spaceKey).
		SetProjectKey(b.projectKey).
		SetRepoName(b.repoName).
		IssueListURL(statusIds))
}

func (b *BacklogRepository) BlamePR(argv []string) error {
	argv = append([]string{"blame", "--first-parent"}, argv...)
	cmd := exec.CommandContext(context.Background(), "git", argv...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return err
	}
	defer func() {
		_ = cmd.Wait()
	}()
	cached := make(map[string]string)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		commitAndSrc := strings.SplitN(scanner.Text(), " ", 2)
		commit, src := commitAndSrc[0], commitAndSrc[1]

		if _, ok := cached[commit]; !ok {
			pr, err := lookup(commit)
			if err != nil {
				return err
			}
			cached[commit] = pr
		}

		padding := len(commit)

		if size := len(cached[commit]); padding < size {
			padding = size
		}

		format := "%-" + strconv.Itoa(padding) + "s %s\n"
		fmt.Printf(format, cached[commit], src)
	}
	return err
}

func lookup(commit string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "show", "--oneline", commit)
	out, err := cmd.Output()
	if err != nil {
		return commit, err
	}
	reg := regexp.MustCompile(`^[a-f0-9]+ Merge pull request #([0-9]+) \S+ into \S+`)
	matches := reg.FindStringSubmatch(string(out))
	if len(matches) < 1 {
		return commit, nil
	}
	id, err := strconv.Atoi(matches[1])
	if err != nil {
		return commit, nil
	}
	return fmt.Sprintf("PR #%d", id), nil
}

func openBrowser(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	return err
}
