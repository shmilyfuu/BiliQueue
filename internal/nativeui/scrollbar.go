package nativeui

func scrollbarThumbGeometry(trackHeight, viewportHeight, contentHeight, minimumThumb, scrollY int) (thumbHeight, thumbOffset, maxScroll int) {
	trackHeight = maxInt(1, trackHeight)
	viewportHeight = maxInt(1, viewportHeight)
	contentHeight = maxInt(0, contentHeight)
	maxScroll = maxInt(0, contentHeight-viewportHeight)
	thumbHeight = trackHeight
	if contentHeight > 0 && maxScroll > 0 {
		thumbHeight = clampInt(trackHeight*viewportHeight/contentHeight, minimumThumb, trackHeight)
	}
	scrollY = clampInt(scrollY, 0, maxScroll)
	if maxScroll > 0 && trackHeight > thumbHeight {
		thumbOffset = scrollY * (trackHeight - thumbHeight) / maxScroll
	}
	return
}

func fadedVisual(style ElementVisual, alpha float32) ElementVisual {
	if alpha < 0 {
		alpha = 0
	} else if alpha > 1 {
		alpha = 1
	}
	style.Fill.Opacity = int(float32(style.Fill.Opacity) * alpha)
	style.Border.Opacity = int(float32(style.Border.Opacity) * alpha)
	return style
}
