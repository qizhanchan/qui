package graphs

import "github.com/qizhanchan/qui"

// Accessibility hooks for Chart. Series breakdown is opaque to the
// agent — it sees the chart as a single leaf with title + subtitle.

func (c *Chart) Role() string { return qui.RoleChart }

func (c *Chart) AccessibleName() string {
	if c == nil {
		return ""
	}
	return c.Title
}

func (c *Chart) AccessibleValue() string {
	if c == nil {
		return ""
	}
	return c.SubTitle
}
