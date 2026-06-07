package php

import (
	"bytes"
	"text/template"
)

const poolTmplStr = `[{{.Domain}}]
user = www-data
group = www-data
listen = /var/run/php/php{{.Version}}-fpm-{{.Domain}}.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0666

pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
`

type poolConfig struct {
	Domain  string
	Version string
}

func renderPoolTemplate(domain, version string) string {
	tmpl, err := template.New("pool").Parse(poolTmplStr)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, poolConfig{
		Domain:  domain,
		Version: version,
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
