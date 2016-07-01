package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/go-docopt"
)

func init() {
	Register("discover", runDiscover, `
usage: flynn-host discover [options] <service>...

Return low-level information about a service.

Options:
  --json                 Pretty print response as JSON

Examples:  

  Show information for service 'flynn-host'.

  $ flynn-host discover flynn-host
  SERVICE     ID                                ADDR
  flynn-host  8d0d15d0b14d9efe6316493a658e6b3f  10.0.2.15:1113
  
  $ flynn-host discover --json controller
  [
    {
      "Name": "controller",
      "Leader": {
        "id": "0bbd29d630f63b11ae999b1dabd565f9",
        "addr": "100.100.36.4:80",
        "proto": "http",
        "meta": {
          "AUTH_KEY": "9b7b3b382f13425da3a8cb390f0937b8",
          "FLYNN_APP_ID": "2169984c-c15f-499d-be77-636612425330",
          "FLYNN_JOB_ID": "host-b773161b-3e50-43ec-bb38-5c492c2ba2fa",
          "FLYNN_PROCESS_TYPE": "web",
          "FLYNN_RELEASE_ID": "305a5c6b-7f01-4232-bc1c-74b6a408960d"
        },
        "index": 22
      },
      "Instances": [
        {
          "id": "0bbd29d630f63b11ae999b1dabd565f9",
          "addr": "100.100.36.4:80",
          "proto": "http",
          "meta": {
            "AUTH_KEY": "9b7b3b382f13425da3a8cb390f0937b8",
            "FLYNN_APP_ID": "2169984c-c15f-499d-be77-636612425330",
            "FLYNN_JOB_ID": "host-b773161b-3e50-43ec-bb38-5c492c2ba2fa",
            "FLYNN_PROCESS_TYPE": "web",
            "FLYNN_RELEASE_ID": "305a5c6b-7f01-4232-bc1c-74b6a408960d"
          },
          "index": 22
        }
      ],
      "Meta": {
        "Data": {},
        "index": 0
      }
    }
  ]  
`)
}

type serviceInfo struct {
	Name      string
	Leader    *discoverd.Instance
	Instances []*discoverd.Instance
	Meta      serviceMeta
}

type serviceMeta struct {
	Data  map[string]interface{}
	Index uint64 `json:"index"`
}

func runDiscover(args *docopt.Args, c *cluster.Client) error {
	services := args.All["<service>"].([]string)

	if !args.Bool["--json"] {
		w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
		defer w.Flush()
		listRec(w, "SERVICE", "ID", "ADDR")
		for _, service := range services {
			svc := discoverd.NewService(service)
			instances, _ := svc.Instances()
			for _, inst := range instances {
				listRec(w, service, inst.ID, inst.Addr)
			}
		}
		return nil
	}

	response := make([]serviceInfo, 0, len(services))

	for _, service := range services {
		svc := discoverd.NewService(service)
		leader, err := svc.Leader()
		if err != nil {
			return err
		}
		instances, err := svc.Instances()
		if err != nil {
			return err
		}
		meta, _ := svc.GetMeta()

		data := make(map[string]interface{})
		if meta.Data != nil {
			if err := json.Unmarshal(meta.Data, &data); err != nil {
				return err
			}
		}

		response = append(response, serviceInfo{
			Name:      service,
			Leader:    leader,
			Instances: instances,
			Meta: serviceMeta{
				Index: meta.Index,
				Data:  data,
			},
		})
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
