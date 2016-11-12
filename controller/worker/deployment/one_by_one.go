package deployment

import "sort"

func (d *DeployJob) deployOneByOne() error {
	log := d.logger.New("fn", "deployOneByOne")
	log.Info("starting one-by-one deployment")

	processTypes := make([]string, 0, len(d.Processes))
	for typ := range d.Processes {
		processTypes = append(processTypes, typ)
	}
	sort.Sort(sort.StringSlice(processTypes))

	for _, typ := range processTypes {
		if err := d.scaleOneByOne(typ, log); err != nil {
			return err
		}
	}

	log.Info("finished one-by-one deployment")
	return nil
}
