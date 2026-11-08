package calc

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/geo"
)

type (
	calendarDate  = calendar.Date
	summaryVATRow = summary.VATRow
)

func geoCm(v int64) geo.Cm { return geo.Cm(v) }
