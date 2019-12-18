package deployment

import (
	"fmt"
	"sort"
	"time"

	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/cluster"
)

func (d *DeployJob) deployInBatches() error {
	log := d.logger.New("fn", "deployInBatches")
	log.Info("starting in-batches deployment")

	batchSize := d.DeployBatchSize
	if batchSize == nil {
		log.Info("batch size not set, using number of hosts")
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
		batchSize = &hostCount
	}

	processTypes := make([]string, 0, len(d.Processes))
	for typ := range d.Processes {
		processTypes = append(processTypes, typ)
	}
	sort.Sort(sort.StringSlice(processTypes))

	log.Info("scaling in batches", "size", *batchSize)

	for _, typ := range processTypes {
		if err := d.scaleUpDownInBatches(typ, *batchSize, log); err != nil {
			return err
		}
	}

	log.Info("finished in-batches deployment")
	return nil
}
