package deployment

import "sort"

func (d *DeployJob) deployOneDownOneUp() error {
	log := d.logger.New("fn", "deployOneDownOneUp")
	log.Info("starting one-down-one-up deployment")

	processTypes := make([]string, 0, len(d.Processes))
	for typ := range d.Processes {
		processTypes = append(processTypes, typ)
	}
	sort.Sort(sort.StringSlice(processTypes))

	for _, typ := range processTypes {
		if err := d.scaleOneDownOneUp(typ, log); err != nil {
			return err
		}
	}

	log.Info("finished one-down-one-up deployment")
	return nil
}
