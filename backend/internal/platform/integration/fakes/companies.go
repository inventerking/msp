package fakes

import (
	"context"
	"fmt"
	"strings"

	"mspengine/internal/platform/integration"
)

// SampleCompanies is invented data on reserved .example domains, so it can never be mistaken for a
// real company. Licences alternate, so licence rules are exercised.
func SampleCompanies() *CompanyData {
	type c struct {
		name, country, industry string
		employees               int
		tech                    string
	}
	data := []c{
		{"Northwind Legal", "GB", "legal_services", 45, "m365,azure"}, {"Harbor Dental Group", "GB", "healthcare", 120, "m365"},
		{"Bluefield Accounting", "GB", "accounting", 30, "gws"}, {"Crestline Manufacturing", "GB", "manufacturing", 410, "m365,sap"},
		{"Riverside Clinic", "US", "healthcare", 85, "m365"}, {"Summit Law Partners", "US", "legal_services", 60, "m365,azure"},
		{"Pioneer Logistics", "US", "logistics", 900, "aws"}, {"Granite Schools Trust", "US", "education", 300, "gws"},
		{"Lindenhof Steuerberatung", "DE", "accounting", 25, "m365"}, {"Rheinwerk Fertigung", "DE", "manufacturing", 150, "sap"},
		{"Maple Street Physio", "CA", "healthcare", 18, ""}, {"Coastal Realty", "AU", "real_estate", 40, "m365"},
		{"Oakridge Nonprofit Alliance", "GB", "nonprofit", 22, "m365"}, {"Kestrel Engineering", "GB", "construction", 75, ""},
		// Invented managed service providers, so competitor discovery has something to find.
		{"Brightbyte IT Services", "GB", "technology", 35, "m365,azure"}, {"Cobalt Managed Services", "GB", "technology", 60, "m365"},
		{"Lakeside Tech Support", "US", "technology", 25, "gws"},
	}
	out := &CompanyData{}
	for i, d := range data {
		domain := strings.ToLower(strings.NewReplacer(" ", "-", "ü", "u").Replace(d.name)) + ".example"
		lic := "public"
		if i%2 == 1 {
			lic = "licensed_tenant"
		}
		var tech []string
		if d.tech != "" {
			tech = strings.Split(d.tech, ",")
		}
		out.Records = append(out.Records, integration.CompanyRecord{ProviderID: fmt.Sprintf("fake-%03d", i+1), Name: d.name, Domain: domain, Country: d.country, Industry: d.industry, Employees: d.employees, Technology: tech, Licence: lic})
	}
	return out
}

// FindContacts invents two contacts per known company.
func (f *CompanyData) FindContacts(_ context.Context, domain string) ([]integration.ContactRecord, error) {
	for _, r := range f.Records {
		if r.Domain == domain {
			return []integration.ContactRecord{
				{FullName: "Alex Morgan", JobTitle: "Managing Director", Email: "alex.morgan@" + domain, DecisionMaker: true, Confidence: 0.9, Licence: "licensed_tenant"},
				{FullName: "Sam Patel", JobTitle: "Office Manager", Email: "sam.patel@" + domain, Confidence: 0.7, Licence: "licensed_tenant"},
			}, nil
		}
	}
	return nil, nil
}
