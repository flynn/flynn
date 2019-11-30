package deployment

import (
	"fmt"
	"sort"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
)

func (d *DeployJob) deployOnePerHost() error {
	log := d.logger.New("fn", "deployOnePerHost")
	log.Info("starting one-per-host deployment")

	log.Info("determining number of hosts")
	hostCount := 0
	hostAttempts := attempt.Strategy{
		Total: 10 * time.Second,
		Delay: 100 * time.Millisecond,
	}
	if err := hostAttempts.Run(func() error {
		hosts, err := cluster.NewClient().Hosts()
		if err != nil {
			return fmt.Errorf("error determining number of hosts: %s", err)
		}
		hostCount = len(hosts)
		return nil
	}); err != nil {
		return err
	}

	processTypes := make([]string, 0, len(d.Processes))
	for typ := range d.Processes {
		processTypes = append(processTypes, typ)
	}
	sort.Sort(sort.StringSlice(processTypes))

	log.Info("scaling one per host", "host_count", hostCount)

	for _, typ := range processTypes {
		if err := d.scaleUpDownInBatches(typ, hostCount, log); err != nil {
			return err
		}
	}

	log.Info("finished one-per-host deployment")
	return nil
}
