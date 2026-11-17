package maintenancetask

import "testing"

func TestNginxVendorModuleRequiresExactPackagedJammyDeclaration(t *testing.T) {
	for path, module := range jammyNginxModules {
		raw := []byte("load_module modules/" + module + ";\n")
		p := newProfile(CertificateRenewal)
		p.External["certbot-platform-profile"] = "ubuntu-22.04:certbot-1.21.0:python-3.10"
		p.Files[path] = digest(raw)
		if got, err := nginxVendorModule(&p, path, raw); err != nil || got != module {
			t.Fatal("stock packaged declaration refused", path, got, err)
		}
		for _, altered := range []string{
			"load_module /usr/lib/nginx/modules/" + module + ";\n",
			"load_module modules/" + module + ";\nenv LD_PRELOAD;\n",
			"load_module \"modules/" + module + "\";\n",
			"load_module modules/ngx_http_lua_module.so;\n",
		} {
			// Even a manifest-backed alternate declaration needs a separate
			// review; package integrity alone never broadens the module map.
			p.Files[path] = digest([]byte(altered))
			if _, err := nginxVendorModule(&p, path, []byte(altered)); err == nil {
				t.Fatal("alternate executable declaration accepted", altered)
			}
		}
		p.Files[path] = digest([]byte("different packaged bytes"))
		if _, err := nginxVendorModule(&p, path, raw); err == nil {
			t.Fatal("changed vendor config accepted")
		}
		delete(p.Files, path)
		if _, err := nginxVendorModule(&p, path, raw); err == nil {
			t.Fatal("unverified vendor config accepted")
		}
		p.Files[path] = digest(raw)
		p.External["certbot-platform-profile"] = "ubuntu-24.04:certbot-2.9.0:python-3.12"
		if _, err := nginxVendorModule(&p, path, raw); err == nil {
			t.Fatal("Jammy module admission crossed platform boundary")
		}
		p.External["certbot-platform-profile"] = "ubuntu-22.04:certbot-1.21.0:python-3.10"
		alias := "/etc/nginx/custom.conf"
		p.Files[alias] = digest(raw)
		if _, err := nginxVendorModule(&p, alias, raw); err == nil {
			t.Fatal("copied executable declaration accepted")
		}
	}
}

func TestNginxRuntimeModulePrefixMustMatchTheReviewedLocation(t *testing.T) {
	if err := nginxModulePrefix("nginx version: nginx/1.18.0\nconfigure arguments: --prefix=/usr/share/nginx --with-compat\n"); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"", "--prefix=/home/fluxo", "--prefix=/usr/share/nginx --prefix=/home/fluxo", "--prefix=/usr/share/nginx/../other", "--prefix='/usr/share/nginx'"} {
		if nginxModulePrefix(output) == nil {
			t.Fatal("unreviewed module prefix accepted", output)
		}
	}
}
