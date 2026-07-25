package nativeui

import "strings"

type controlState uint8

const (
	controlNormal controlState = iota
	controlHover
	controlPressed
)

func buttonState(hovered, pressed bool) controlState {
	if pressed {
		return controlPressed
	}
	if hovered {
		return controlHover
	}
	return controlNormal
}

func buttonVisual(style ElementVisual, state controlState) ElementVisual {
	color := strings.ToUpper(strings.TrimSpace(style.Fill.Color1))
	switch color {
	case "#60CDFF", "#AFE6FF", "#0078D4":
		switch state {
		case controlHover:
			setSolidFill(&style, "#AFE6FF", 100)
		case controlPressed:
			setSolidFill(&style, "#0078D4", 100)
		default:
			setSolidFill(&style, "#60CDFF", 100)
		}
	case "#C42B1C":
		opacity := 50
		if state == controlHover {
			opacity = 70
		} else if state == controlPressed {
			opacity = 90
		}
		setSolidFill(&style, "#C42B1C", opacity)
	default:
		opacity := 6
		if state == controlHover {
			opacity = 8
		} else if state == controlPressed {
			opacity = 4
		}
		setSolidFill(&style, "#FFFFFF", opacity)
	}
	return style
}

func dangerButtonVisual(style ElementVisual, state controlState) ElementVisual {
	opacity := 50
	if state == controlHover {
		opacity = 70
	} else if state == controlPressed {
		opacity = 90
	}
	setSolidFill(&style, "#C42B1C", opacity)
	style.TextColor = "#FFFFFF"
	return style
}

func setSolidFill(style *ElementVisual, color string, opacity int) {
	style.FillDisabled = false
	style.Fill.Type = "solid"
	style.Fill.Color1 = color
	style.Fill.Color2 = color
	style.Fill.Opacity = clampInt(opacity, 0, 100)
	style.Fill.Stops = []GradientStop{
		{Position: 0, Color: color, Opacity: 100},
		{Position: 100, Color: color, Opacity: 100},
	}
}

func buttonContentRect(rect Rect, state controlState) Rect {
	if state == controlPressed {
		rect.Y++
	}
	return rect
}
