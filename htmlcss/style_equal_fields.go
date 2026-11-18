package htmlcss

import (
	"reflect"
	"slices"
)

// stylesEqualFields compares the non-variable, non-state part without
// reflecting over hundreds of scalar fields on each style read. The field
// coverage test fails if a new ComputedStyle field is omitted here.
const styleEqualFieldCount = 164

func stylesEqualFields(a, b *ComputedStyle) bool {
	if a.HyphensAuto != b.HyphensAuto ||
		a.Language != b.Language ||
		a.Display != b.Display ||
		a.Hidden != b.Hidden ||
		a.Color != b.Color ||
		a.HasColor != b.HasColor ||
		a.ColorOwn != b.ColorOwn ||
		a.AccentColor != b.AccentColor ||
		a.HasAccentColor != b.HasAccentColor ||
		a.ScrollbarThumb != b.ScrollbarThumb ||
		a.ScrollbarTrack != b.ScrollbarTrack ||
		a.HasScrollbarColor != b.HasScrollbarColor ||
		a.ScrollbarNone != b.ScrollbarNone ||
		a.FontSize != b.FontSize ||
		a.FontWeight != b.FontWeight ||
		a.Italic != b.Italic ||
		a.FontFamily != b.FontFamily ||
		a.FontSizeKeyword != b.FontSizeKeyword ||
		a.FontMonoOnly != b.FontMonoOnly ||
		a.RootFontSize != b.RootFontSize ||
		a.LineHeight != b.LineHeight ||
		a.LineHeightPx != b.LineHeightPx ||
		a.LineHeightOwn != b.LineHeightOwn ||
		a.Direction != b.Direction ||
		a.TextAlign != b.TextAlign ||
		a.Float != b.Float ||
		a.Clear != b.Clear ||
		a.CenterBlocks != b.CenterBlocks ||
		a.LetterSpacing != b.LetterSpacing ||
		a.WordSpacing != b.WordSpacing ||
		a.TextIndent != b.TextIndent ||
		a.Underline != b.Underline ||
		a.LineThrough != b.LineThrough ||
		a.Overline != b.Overline ||
		a.DecorationColor != b.DecorationColor ||
		a.HasDecorationColor != b.HasDecorationColor ||
		a.DecorationStyle != b.DecorationStyle ||
		a.DecorationThickness != b.DecorationThickness ||
		a.UnderlineOffset != b.UnderlineOffset ||
		a.HasUnderlineOffset != b.HasUnderlineOffset ||
		a.TextBalance != b.TextBalance ||
		a.NoWrap != b.NoWrap ||
		a.PreserveWS != b.PreserveWS ||
		a.PreserveNL != b.PreserveNL ||
		a.Ellipsis != b.Ellipsis ||
		a.TextTransform != b.TextTransform ||
		a.BreakWord != b.BreakWord ||
		a.Background != b.Background ||
		a.HasBackground != b.HasBackground ||
		a.BorderColor != b.BorderColor ||
		a.BorderWidth != b.BorderWidth ||
		a.BorderStyle != b.BorderStyle ||
		a.HasBorder != b.HasBorder ||
		a.HasSideBorders != b.HasSideBorders ||
		a.SideWidths != b.SideWidths ||
		a.SideColors != b.SideColors ||
		a.Radius != b.Radius ||
		a.widthExpr != b.widthExpr ||
		a.heightExpr != b.heightExpr ||
		a.Corners != b.Corners ||
		a.Padding != b.Padding ||
		a.Margin != b.Margin ||
		a.OutlineWidth != b.OutlineWidth ||
		a.OutlineColor != b.OutlineColor ||
		a.HasOutline != b.HasOutline ||
		a.OutlineNone != b.OutlineNone ||
		a.Width != b.Width ||
		a.HasWidth != b.HasWidth ||
		a.Height != b.Height ||
		a.HasHeight != b.HasHeight ||
		a.HeightDefinite != b.HeightDefinite ||
		a.HeightIntrinsicAuto != b.HeightIntrinsicAuto ||
		a.WidthPct != b.WidthPct ||
		a.HeightPct != b.HeightPct ||
		a.MarginLeftAuto != b.MarginLeftAuto ||
		a.MarginRightAuto != b.MarginRightAuto ||
		a.MarginTopAuto != b.MarginTopAuto ||
		a.MarginBottomAuto != b.MarginBottomAuto ||
		a.MinWidth != b.MinWidth ||
		a.MinHeight != b.MinHeight ||
		a.MaxWidth != b.MaxWidth ||
		a.MaxHeight != b.MaxHeight ||
		a.MinWidthPct != b.MinWidthPct ||
		a.MinHeightPct != b.MinHeightPct ||
		a.MaxWidthPct != b.MaxWidthPct ||
		a.MaxHeightPct != b.MaxHeightPct ||
		a.BoxSizing != b.BoxSizing ||
		a.FlexDirection != b.FlexDirection ||
		a.Justify != b.Justify ||
		a.AlignItems != b.AlignItems ||
		a.JustifyItems != b.JustifyItems ||
		a.RowGap != b.RowGap ||
		a.ColumnGap != b.ColumnGap ||
		a.Wrap != b.Wrap ||
		a.AlignContent != b.AlignContent ||
		a.FlexGrow != b.FlexGrow ||
		a.AlignSelf != b.AlignSelf ||
		a.JustifySelf != b.JustifySelf ||
		a.FlexShrink != b.FlexShrink ||
		a.HasFlexShrink != b.HasFlexShrink ||
		a.FlexBasis != b.FlexBasis ||
		a.HasFlexBasis != b.HasFlexBasis ||
		a.FlexBasisAuto != b.FlexBasisAuto ||
		a.Order != b.Order ||
		a.Shadow != b.Shadow ||
		a.HasShadow != b.HasShadow ||
		a.Opacity != b.Opacity ||
		a.HasOpacity != b.HasOpacity ||
		a.ListItem != b.ListItem ||
		a.ListStyleType != b.ListStyleType ||
		a.BackgroundImageURL != b.BackgroundImageURL ||
		a.Position != b.Position ||
		a.ZIndexSet != b.ZIndexSet ||
		a.ZIndexItem != b.ZIndexItem ||
		a.AspectRatio != b.AspectRatio ||
		a.WidthPctOffset != b.WidthPctOffset ||
		a.HeightPctOffset != b.HeightPctOffset ||
		a.FlexBasisPct != b.FlexBasisPct ||
		a.FlexBasisPctOffset != b.FlexBasisPctOffset ||
		a.InlineStatic != b.InlineStatic ||
		a.Top != b.Top ||
		a.Right != b.Right ||
		a.Bottom != b.Bottom ||
		a.Left != b.Left ||
		a.HasTop != b.HasTop ||
		a.HasRight != b.HasRight ||
		a.HasBottom != b.HasBottom ||
		a.HasLeft != b.HasLeft ||
		a.InsetPct != b.InsetPct ||
		a.ZIndex != b.ZIndex ||
		a.OverflowClip != b.OverflowClip ||
		a.OverflowClipX != b.OverflowClipX ||
		a.OverflowClipY != b.OverflowClipY ||
		a.OverflowScroll != b.OverflowScroll ||
		a.OverflowScrollX != b.OverflowScrollX ||
		a.OverflowScrollXY != b.OverflowScrollXY ||
		a.AppRegion != b.AppRegion ||
		a.Cursor != b.Cursor ||
		a.HasCursor != b.HasCursor ||
		a.NoSelect != b.NoSelect ||
		a.PointerNone != b.PointerNone ||
		a.FocusOptIn != b.FocusOptIn {
		return false
	}
	if (a.LigatureFeatures == nil) != (b.LigatureFeatures == nil) || !slices.Equal(a.LigatureFeatures, b.LigatureFeatures) {
		return false
	}
	if (a.FeatureSettings == nil) != (b.FeatureSettings == nil) || !slices.Equal(a.FeatureSettings, b.FeatureSettings) {
		return false
	}
	if (a.ExtraShadows == nil) != (b.ExtraShadows == nil) || !slices.Equal(a.ExtraShadows, b.ExtraShadows) {
		return false
	}
	if (a.TextShadows == nil) != (b.TextShadows == nil) || !slices.Equal(a.TextShadows, b.TextShadows) {
		return false
	}
	if (a.Counters == nil) != (b.Counters == nil) || !slices.Equal(a.Counters, b.Counters) {
		return false
	}
	if a.borderRadii != b.borderRadii && !reflect.DeepEqual(a.borderRadii, b.borderRadii) {
		return false
	}
	if a.Transform != b.Transform && !reflect.DeepEqual(a.Transform, b.Transform) {
		return false
	}
	if a.Gradient != b.Gradient && !reflect.DeepEqual(a.Gradient, b.Gradient) {
		return false
	}
	if (a.Filter != nil || b.Filter != nil) && !reflect.DeepEqual(a.Filter, b.Filter) {
		return false
	}
	if (a.BackdropFilter != nil || b.BackdropFilter != nil) && !reflect.DeepEqual(a.BackdropFilter, b.BackdropFilter) {
		return false
	}
	if a.ClipPath != b.ClipPath && !reflect.DeepEqual(a.ClipPath, b.ClipPath) {
		return false
	}
	if a.Mask != b.Mask && !reflect.DeepEqual(a.Mask, b.Mask) {
		return false
	}
	return true
}
