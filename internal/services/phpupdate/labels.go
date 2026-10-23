package phpupdate

import (
	"fmt"

	"fluxo/internal/services/site"
)

func consumerLabel(c Consumer, sites []Site) string {
	for _, s := range sites {
		if s.ID == c.SiteID {
			return site.MaintenanceLabel(c.Name, s.Domain)
		}
	}
	return site.MaintenanceLabel(c.Name, "")
}

func runtimeLabel(p Plan, name string) string {
	if name == "php"+p.Version+"-fpm.service" {
		return "PHP " + p.Version + "-FPM"
	}
	for _, c := range p.Impact.Workers {
		for i := 1; i <= c.Instances; i++ {
			if unitName(c.ID, i) == name {
				label := consumerLabel(c, p.Impact.Sites)
				if c.Instances > 1 {
					label += fmt.Sprintf(" (instance %d)", i)
				}
				return label
			}
		}
	}
	return "PHP background service"
}

func savedRuntimeLabel(planID, name string) string {
	if p, err := GetPlan(planID); err == nil {
		return runtimeLabel(p, name)
	}
	return "PHP background service"
}
