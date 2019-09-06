package challonge

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	API_VERSION = "v1"
	tournaments = "tournaments"
	STATE_OPEN  = "open"
	STATE_ALL   = "all"
)

var client *Client
var debug = false

type tournament Tournament

type Client struct {
	baseURL string
	key     string
	version string
	user    string
}

type APIResponse struct {
	Tournament  *Tournament  `json:"tournament"`
	Participant *Participant `json:"participant"`
	Match       Match        `json:"match"`

	Errors []string `json:"errors"`
}

type Tournament struct {
	Name              string    `json:"name"`
	ID                int       `json:"id"`
	URL               string    `json:"url"`
	FullURL           string    `json:"full_challonge_url"`
	State             string    `json:"state"`
	SubDomain         string    `json:"subdomain"`
	ParticipantsCount int       `json:"participants_count"`
	StartedAt         time.Time `json:"started_at,mitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
	Type              string    `json:"tournament_type"`
	Description       string    `json:"description"`
	GameName          string    `json:"game_name"`
	Progress          int       `json:"progress_meter"`
	CompletedAt       time.Time `json:"completed_at"`

	SubURL string `json:"sub_url"`

	ParticipantItems []*ParticipantItem `json:"participants,omitempty"`
	MatchItems       []*MatchItem       `json:"matches,omitempty"`

	Participants []*Participant `json:"resolved_participants"`
	Matches      []*Match       `json:"resolved_matches"`
}

type Participant struct {
	ID             int    `json:"id"`
	Name           string `json:"display_name"`
	Misc           string `json:"misc"`
	Seed           int    `json:"seed"`
	Wins           int
	Losses         int
	TotalScore     int
	FinalRank      int   `json:"final_rank"`
	GroupPlayerIds []int `json:"group_player_ids"`
}

type Match struct {
	ID             int    `json:"id"`
	Identifier     string `json:"identifier"`
	State          string `json:"state"`
	PlayerOneID    int    `json:"player1_id"`
	PlayerTwoID    int    `json:"player2_id"`
	PlayerOneScore int
	PlayerTwoScore int
	UpdatedAt      time.Time `json:"updated_at,omitempty"`

	WinnerID int `json:"winner_id"`
	LoserID  int `json:"loser_id"`

	PlayerOne   *Participant
	PlayerTwo   *Participant
	Winner      *Participant
	Loser       *Participant
	WinnerScore int
	LoserScore  int

	Scores string `json:"scores_csv"`
}

/** items to flatten json structure */
type TournamentItem struct {
	Tournament Tournament `json:"tournament"`
}

type ParticipantItem struct {
	Participant Participant `json:"participant"`
}

type MatchItem struct {
	Match *Match `json:"match"`
}

type TournamentRequest struct {
	client *Client
	ID     string
	Params map[string]string
}

func (c *Client) Print() {
	log.Print(c.key)
}

func New(user string, key string) *Client {
	client = &Client{user: user, version: API_VERSION, key: key}
	return client
}

func (c *Client) Debug() {
	debug = true
}

func (c *Client) buildUrl(route string, v url.Values) string {
	url := fmt.Sprintf("https://%s:%s@api.challonge.com/%s/%s.json", c.user, c.key, c.version, route)
	if v != nil {
		url += "?" + v.Encode()
	}

	return url
}

func params(p map[string]string) *url.Values {
	values := url.Values{}
	for k, v := range p {
		values.Add(k, v)
	}
	return &values
}

func (r *APIResponse) hasErrors() bool {
	if debug {
		log.Printf("response had errors: %q", r.Errors)
	}
	return len(r.Errors) > 0
}

func (r *APIResponse) getTournament() *Tournament {
	return r.Tournament.resolveRelations()
}

func (c *Client) NewTournamentRequest(id string) *TournamentRequest {
	return &TournamentRequest{client: c, ID: id, Params: make(map[string]string, 0)}
}

func (r *TournamentRequest) WithParticipants() *TournamentRequest {
	r.Params["include_participants"] = "1"
	return r
}

func (r *TournamentRequest) WithMatches() *TournamentRequest {
	r.Params["include_matches"] = "1"
	return r
}

func (t *Tournament) Update() *TournamentRequest {
	return client.NewTournamentRequest(t.SubURL)
}

func (r *TournamentRequest) Get() (*Tournament, error) {
	url := r.client.buildUrl("tournaments/"+r.ID, *params(r.Params))
	response := &APIResponse{}
	doGet(url, response)
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("unable to retrieve tournament: %q", response.Errors[0])
	}
	if response.Tournament.State != "complete" {
		return nil, fmt.Errorf("tournament state is not 'completed'")
	}
	tournament := response.getTournament()
	tournament.SubURL = r.ID
	return tournament, nil
}

/** creates a new tournament */
func (c *Client) CreateTournament(name string, subUrl string, domain string, open bool, tType string) (*Tournament, error) {
	v := *params(map[string]string{
		"tournament[name]":        name,
		"tournament[url]":         subUrl,
		"tournament[open_signup]": "false",
		"tournament[subdomain]":   domain,
	})
	if tType == "" || tType == "single" {
		v.Add("tournament[tournament_type]", "single elimination")
	} else if tType == "double" {
		v.Add("tournament[tournament_type]", "double elimination")
	}
	url := c.buildUrl("tournaments", v)
	response := &APIResponse{}
	doPost(url, response)
	if response.hasErrors() {
		return nil, fmt.Errorf("unable to create tournament: %q", response.Errors[0])
	}
	return response.getTournament(), nil
}

func (t *Tournament) Start() error {
	v := *params(map[string]string{
		"include_participants": "1",
		"include_matches":      "1",
	})
	url := client.buildUrl("tournaments/"+t.GetUrl()+"/start", v)
	response := &APIResponse{}
	doPost(url, response)
	if response.hasErrors() {
		return fmt.Errorf("error starting tournament:  %q", response.Errors[0])
	}
	tournament := response.getTournament()
	if tournament.State == "underway" {
		if debug {
			log.Printf("tournament %q started", tournament.Name)
		}
	} else {
		return fmt.Errorf("tournament has state %q, probably not started", tournament.State)
	}
	t = tournament
	return nil
}

func (t *Tournament) SubmitMatch(m *Match) (*Match, error) {
	v := *params(map[string]string{
		"match[scores_csv]": fmt.Sprintf("%d-%d", m.PlayerOneScore, m.PlayerTwoScore),
		"match[winner_id]":  fmt.Sprintf("%d", m.WinnerID),
	})
	url := client.buildUrl(fmt.Sprintf("tournaments/%s/matches/%d", t.GetUrl(), m.ID), v)
	response := &APIResponse{}
	doPut(url, response)
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("%q", response.Errors[0])
	}
	m = &response.Match
	return &response.Match, nil
}

/** adds participant to tournament */
func (t *Tournament) AddParticipant(name string, misc string) (*Participant, error) {
	v := *params(map[string]string{
		"participant[name]": name,
		"participant[misc]": misc,
	})
	url := client.buildUrl("tournaments/"+t.GetUrl()+"/participants", v)
	response := &APIResponse{}
	doPost(url, response)
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("unable to add participant: %q", response.Errors[0])
	}
	t.Participants = append(t.Participants, response.Participant)
	return response.Participant, nil
}

/** returns "domain-url" or "url" */
func (t *Tournament) GetUrl() string {
	if t.SubDomain != "" {
		return t.SubDomain + "-" + t.URL
	}
	return t.URL
}

/** removes participant from tournament */
func (t *Tournament) RemoveParticipant(name string) error {
	p := t.GetParticipantByName(name)
	if p == nil || p.ID == 0 {
		return fmt.Errorf("participant with name %q not found in tournament", name)
	}
	return t.RemoveParticipantById(p.ID)
}

/** removes participant by id */
func (t *Tournament) RemoveParticipantById(id int) error {
	url := client.buildUrl("tournaments/"+t.GetUrl()+"/participants/"+strconv.Itoa(id), nil)
	response := &APIResponse{}
	doDelete(url, response)
	if len(response.Errors) > 0 {
		return fmt.Errorf("unable to delete participant: %q", response.Errors[0])
	}
	return nil
}

/** returns a participant id based on name */
type cmp func(*Participant) bool

func (t *Tournament) GetParticipant(id int) *Participant {
	return t.getParticipantByCmp(func(p *Participant) bool { return p.ID == id })
}
func (t *Tournament) GetParticipantByName(name string) *Participant {
	return t.getParticipantByCmp(func(p *Participant) bool { return p.Name == name })
}
func (t *Tournament) GetParticipantByMisc(misc string) *Participant {
	return t.getParticipantByCmp(func(p *Participant) bool { return p.Misc == misc })
}
func (t *Tournament) getParticipantByGroupPlayerId(id int) *Participant {
	return t.getParticipantByCmp(func(p *Participant) bool { return p.GroupPlayerIds[0] == id })
}

func (t *Tournament) getParticipantByCmp(cmp cmp) *Participant {
	for _, p := range t.Participants {
		if cmp(p) {
			return p
		}
	}
	return nil
}

/** returns all matches for tournament */
func (t *Tournament) GetMatches() []*Match {
	return t.getMatches(STATE_ALL)
}

/** returns all open matches */
func (t *Tournament) GetOpenMatches() []*Match {
	return t.getMatches(STATE_OPEN)
}

/** resolves and returns matches for tournament */
func (t *Tournament) getMatches(state string) []*Match {
	matches := make([]*Match, 0)

	for _, m := range t.Matches {
		m.ResolveParticipants(t)
		if state == STATE_ALL {
			matches = append(matches, m)
		} else if m.State == state {
			matches = append(matches, m)
		}
	}
	return matches
}

/** returns match with resolved participants */
func (t *Tournament) GetMatch(id int) *Match {
	for _, match := range t.Matches {
		if match.ID == id {
			match.ResolveParticipants(t)
			return match
		}
	}
	return nil
}

func (t *Tournament) IsCompleted() bool {
	return t.State == "complete" || t.State == "awaiting_review"
}

func (t *Tournament) GetOpenMatchForParticipant(p *Participant) *Match {
	matches := t.GetOpenMatches()
	for _, m := range matches {
		if m.PlayerOneID == p.ID || m.PlayerTwoID == p.ID {
			return m
		}
	}
	return nil
}

func (p *Participant) Lose() {
	p.Losses += 1
}

func (p *Participant) Win() {
	p.Wins += 1
}

func separateScores(score string) (int, int, error) {
	sep := strings.Split(score, "-")
	if len(sep) != 2 {
		return 0, 0, fmt.Errorf("score is in wrong format")
	}
	a, err := strconv.Atoi(sep[0])
	if err != nil {
		return 0, 0, fmt.Errorf("could not format scores: %v", err)
	}

	b, err := strconv.Atoi(sep[1])
	if err != nil {
		return 0, 0, fmt.Errorf("could not format scores: %v", err)
	}
	return a, b, nil
}

func (m *Match) ResolveParticipants(t *Tournament) {
	m.PlayerOne = t.GetParticipant(m.PlayerOneID)
	m.PlayerTwo = t.GetParticipant(m.PlayerTwoID)

	if m.PlayerOne == nil {
		m.PlayerOne = t.getParticipantByGroupPlayerId(m.PlayerOneID)
	}

	if m.PlayerTwo == nil {
		m.PlayerTwo = t.getParticipantByGroupPlayerId(m.PlayerTwoID)
	}

	scoreOne, scoreTwo, err := separateScores(m.Scores)

	if err != nil {
		m.PlayerOneScore = 0
		m.PlayerTwoScore = 0
	}

	m.PlayerOneScore = scoreOne
	m.PlayerTwoScore = scoreTwo

	m.PlayerOne.TotalScore += m.PlayerOneScore
	m.PlayerTwo.TotalScore += m.PlayerTwoScore

	if m.WinnerID == m.PlayerOneID {
		m.PlayerOne.Win()
		m.PlayerTwo.Lose()
		m.WinnerScore = m.PlayerOneScore
		m.LoserScore = m.PlayerTwoScore
		m.Winner = m.PlayerOne
		m.Loser = m.PlayerTwo

	} else if m.WinnerID == m.PlayerTwoID {
		m.PlayerTwo.Win()
		m.PlayerOne.Lose()
		m.WinnerScore = m.PlayerTwoScore
		m.LoserScore = m.PlayerOneScore
		m.Winner = m.PlayerTwo
		m.Loser = m.PlayerOne
	}
}

func (t *Tournament) resolveRelations() *Tournament {
	participants := make([]*Participant, 0)
	for _, item := range t.ParticipantItems {
		participants = append(participants, &item.Participant)
	}
	t.Participants = participants
	t.ParticipantItems = nil

	matches := make([]*Match, 0)
	for _, item := range t.MatchItems {
		match := item.Match
		if match.State == "pending" {
			continue
		}
		match.ResolveParticipants(t)
		matches = append(matches, match)
	}
	t.Matches = matches
	t.MatchItems = nil

	return t
}

func DiffMatches(matches1 []*Match, matches2 []*Match) []*Match {
	diff := make([]*Match, 0)

	for i, _ := range matches1 {
		if i >= len(matches2) {
			break
		}
		if matches1[i].State != matches2[i].State {
			diff = append(diff, matches2[i])
		}
	}

	return diff
}

func doGet(url string, v *APIResponse) {
	if debug {
		log.Print("gets resource on url ", url)
	}
	resp, err := http.Get(url)
	if debug {
		log.Print("got headers ", resp)
	}
	if err != nil {
		log.Fatal("unable to get resource ", err)
	}
	handleResponse(resp, v)
}

func doPost(url string, v interface{}) {
	if debug {
		log.Print("posts resource on url ", url)
	}
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		log.Fatal("unable to get resource ", err)
	}
	handleResponse(resp, v)
}

func doPut(url string, v interface{}) {
	req, err := http.NewRequest("PUT", url, nil)
	log.Print("puts resource on url ", url)
	if err != nil {
		log.Fatal("unable to create put request")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal("unable to delete", err)
	}
	handleResponse(resp, v)
}

func doDelete(url string, v interface{}) {
	req, err := http.NewRequest("DELETE", url, nil)
	log.Print("deletes resource on url ", url)
	if err != nil {
		log.Fatal("unable to create delete request")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal("unable to delete", err)
	}
	handleResponse(resp, v)
}

func handleResponse(r *http.Response, v interface{}) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Fatal("unable to read response", err)
	}
	err = json.Unmarshal(body, v)
	if err != nil {
		log.Print("Error unmarshaling json ", err)
	}
	if debug {
		log.Print("unmarshaled to ", v)
	}
}

func (t *Tournament) UnmarshalJSON(b []byte) (err error) {
	placeholder := tournament{}
	if err = json.Unmarshal(b, &placeholder); err == nil {
		*t = Tournament(placeholder)
		return
	}
	return
}
