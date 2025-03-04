package golink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"
)

type LinkDocument struct {
	Id       string  `json:"normalizedId"`
	Short    string  `json:"short"`
	Long     string  `json:"long"`
	Created  float64 `json:"created"`
	LastEdit float64 `json:"lastEdit"`
	Owner    string  `json:"owner"`
}

type StatsMap = map[string]interface{}

type ConvexDB struct {
	url   string
	token string
}

type UdfExecution struct {
	Path   string                 `json:"path"`
	Args   map[string]interface{} `json:"args"`
	Format string                 `json:"format"`
}

type ConvexResponse struct {
	Status       string          `json:"status"`
	Value        json.RawMessage `json:"value"`
	ErrorMessage string          `json:"errorMessage"`
}

func NewConvexDB(url string, token string) *ConvexDB {
	return &ConvexDB{url: url, token: token}
}

func (c *ConvexDB) mutation(args *UdfExecution) error {
	args.Args["token"] = c.token
	url := fmt.Sprintf("%s/api/mutation", c.url)
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(encodedArgs))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code from Convex: %d", resp.StatusCode)
	}

	defer resp.Body.Close()
	var convexResponse ConvexResponse
	err = json.NewDecoder(resp.Body).Decode(&convexResponse)
	if err != nil {
		return err
	}
	if convexResponse.Status == "success" {
		return nil
	}
	if convexResponse.Status == "error" {
		return fmt.Errorf("error from Convex: %s", convexResponse.ErrorMessage)
	}
	return fmt.Errorf("unexpected response from Convex: %s", resp.Body)
}

func (c *ConvexDB) query(args *UdfExecution) (json.RawMessage, error) {
	args.Args["token"] = c.token
	url := fmt.Sprintf("%s/api/query", c.url)
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(encodedArgs))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code from Convex: %d: %s", resp.StatusCode, body)
	}

	defer resp.Body.Close()
	var convexResponse ConvexResponse
	err = json.NewDecoder(resp.Body).Decode(&convexResponse)
	if err != nil {
		return nil, err
	}
	if convexResponse.Status == "success" {
		return convexResponse.Value, nil
	}
	if convexResponse.Status == "error" {
		return nil, fmt.Errorf("error from Convex: %s", convexResponse.ErrorMessage)
	}
	return nil, fmt.Errorf("unexpected response from Convex: %s", resp.Body)
}

func (c *ConvexDB) LoadAll() ([]*Link, error) {
	args := UdfExecution{"load:loadAll", map[string]interface{}{}, "json"}
	resp, err := c.query(&args)
	if err != nil {
		return nil, err
	}
	var docs []LinkDocument
	decoder := json.NewDecoder(bytes.NewReader(resp))
	decoder.UseNumber()
	err = decoder.Decode(&docs)
	if err != nil {
		return nil, err
	}
	var links []*Link
	for _, doc := range docs {
		link := Link{
			Short:    doc.Short,
			Long:     doc.Long,
			Created:  time.Unix(int64(doc.Created), 0),
			LastEdit: time.Unix(int64(doc.LastEdit), 0),
			Owner:    doc.Owner,
		}
		links = append(links, &link)
	}
	return links, nil
}

func (c *ConvexDB) Load(short string) (*Link, error) {
	args := UdfExecution{"load:loadOne", map[string]interface{}{"normalizedId": linkID(short)}, "json"}
	resp, err := c.query(&args)
	if err != nil {
		return nil, err
	}
	var doc *LinkDocument
	decoder := json.NewDecoder(bytes.NewReader(resp))
	decoder.UseNumber()
	err = decoder.Decode(&doc)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		err := fs.ErrNotExist
		return nil, err
	}

	link := Link{
		Short:    doc.Short,
		Long:     doc.Long,
		Created:  time.Unix(int64(doc.Created), 0),
		LastEdit: time.Unix(int64(doc.LastEdit), 0),
		Owner:    doc.Owner,
	}
	return &link, nil
}

func (c *ConvexDB) Save(link *Link) error {
	document := LinkDocument{
		Id:       linkID(link.Short),
		Short:    link.Short,
		Long:     link.Long,
		Created:  float64(link.Created.Unix()),
		LastEdit: float64(link.LastEdit.Unix()),
		Owner:    link.Owner,
	}
	args := UdfExecution{"store", map[string]interface{}{"link": document}, "json"}
	return c.mutation(&args)
}

func (c *ConvexDB) LoadStats() (ClickStats, error) {
	args := UdfExecution{"stats:loadStats", map[string]interface{}{}, "json"}
	response, err := c.query(&args)
	if err != nil {
		return nil, err
	}
	var stats StatsMap
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.UseNumber()
	err = decoder.Decode(&stats)
	if err != nil {
		return nil, err
	}
	clicks := make(ClickStats)
	for k, v := range stats {
		num, err := v.(json.Number).Float64()
		if err != nil {
			return nil, err
		}
		clicks[k] = int(num)
	}
	return clicks, nil
}

func (c *ConvexDB) SaveStats(stats ClickStats) error {
	mungedStats := make(map[string]int)
	for id, clicks := range stats {
		mungedStats[linkID(id)] = clicks
	}
	args := UdfExecution{"stats:saveStats", map[string]interface{}{"stats": mungedStats}, "json"}
	return c.mutation(&args)
}
