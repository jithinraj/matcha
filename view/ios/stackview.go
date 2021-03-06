package ios

import (
	"fmt"
	"image/color"
	"strconv"

	"github.com/gogo/protobuf/proto"
	"gomatcha.io/matcha/comm"
	"gomatcha.io/matcha/internal"
	"gomatcha.io/matcha/layout/constraint"
	pb "gomatcha.io/matcha/proto"
	pbtext "gomatcha.io/matcha/proto/text"
	pbios "gomatcha.io/matcha/proto/view/ios"
	"gomatcha.io/matcha/text"
	"gomatcha.io/matcha/view"
)

// Stack represents a list of views to be shown in the StackView. It can be manipulated outside of a Build() call.
type Stack struct {
	relay       comm.Relay
	childIds    []int64
	childrenMap map[int64]view.View
	maxId       int64
}

func (s *Stack) SetViews(vs ...view.View) {
	if s.childrenMap == nil {
		s.childrenMap = map[int64]view.View{}
	}

	for _, i := range vs {
		s.maxId += 1
		s.childIds = append(s.childIds, s.maxId)
		s.childrenMap[s.maxId] = i
	}
	s.relay.Signal()
}

func (s *Stack) setChildIds(ids []int64) {
	s.childIds = ids
	s.relay.Signal()
}

func (s *Stack) Views() []view.View {
	vs := []view.View{}
	for _, i := range s.childIds {
		vs = append(vs, s.childrenMap[i])
	}
	return vs
}

func (s *Stack) Push(vs view.View) {
	s.maxId += 1

	s.childIds = append(s.childIds, s.maxId)
	s.childrenMap[s.maxId] = vs
	s.relay.Signal()
}

func (s *Stack) Pop() {
	if len(s.childIds) <= 1 {
		return
	}
	delete(s.childrenMap, s.childIds[len(s.childIds)-1])
	s.childIds = s.childIds[:len(s.childIds)-1]
	s.relay.Signal()
}

func (s *Stack) Notify(f func()) comm.Id {
	return s.relay.Notify(f)
}

func (s *Stack) Unnotify(id comm.Id) {
	s.relay.Unnotify(id)
}

/*
Building a simple StackView:

	type AppView struct {
		view.Embed
		stack *ios.Stack
	}
	func NewAppView() *AppView {
		child := view.NewBasicView()
		child.Painter = &paint.Style{BackgroundColor: colornames.Red}
		appview := &AppView{
			stack: &ios.Stack{},
		}
		appview.stack.SetViews(child)
		return appview
	}
	func (v *AppView) Build(ctx view.Context) view.Model {
		child := ios.New()
		child.Stack = v.stack
		return view.Model{
			Children: []view.View{child},
		}
	}

Modifying the stack:

	child := view.NewBasicView()
	child.Painter = &paint.Style{BackgroundColor: colornames.Green}
	v.Stack.Push(child)

*/
type StackView struct {
	view.Embed
	Stack      *Stack
	stack      *Stack
	TitleStyle *text.Style
	BackStyle  *text.Style
	BarColor   color.Color
}

// NewStackView returns a new view.
func NewStackView() *StackView {
	return &StackView{
		Stack: &Stack{},
	}
}

// Lifecyle implements the view.View interface.
func (v *StackView) Lifecycle(from, to view.Stage) {
	if view.EntersStage(from, to, view.StageMounted) {
		if v.Stack == nil {
			v.Stack = &Stack{}
		}
		v.Subscribe(v.Stack)
	} else if view.ExitsStage(from, to, view.StageMounted) {
		v.Unsubscribe(v.Stack)
	}
}

func (v *StackView) Update(v2 view.View) {
	v.Unsubscribe(v.Stack)

	view.CopyFields(v, v2)

	if v.Stack == nil {
		v.Stack = &Stack{}
	}
	v.Subscribe(v.Stack)
}

// Build implements the view.View interface.
func (v *StackView) Build(ctx view.Context) view.Model {
	l := &constraint.Layouter{}

	childrenPb := []*pbios.StackChildView{}
	for _, id := range v.Stack.childIds {
		chld := v.Stack.childrenMap[id]

		// Find the bar.
		var bar *StackBar
		for _, opts := range chld.Build(nil).Options {
			var ok bool
			if bar, ok = opts.(*StackBar); ok {
				break
			}
		}
		if bar == nil {
			bar = &StackBar{
				Title: "Title",
			}
		}

		// Add the bar.
		barV := &stackBarView{
			Embed: view.Embed{Key: strconv.Itoa(int(id))},
			Bar:   bar,
		}
		l.Add(barV, func(s *constraint.Solver) {
			s.Top(0)
			s.Left(0)
			s.WidthEqual(l.MaxGuide().Width())
			s.Height(44)
		})

		// Add the child.
		l.Add(chld, func(s *constraint.Solver) {
			s.Top(0)
			s.Left(0)
			s.WidthEqual(l.MaxGuide().Width())
			s.HeightEqual(l.MaxGuide().Height().Add(-64)) // TODO(KD): Respect bar actual height, shorter when rotated, etc...
		})

		// Add ids to protobuf.
		childrenPb = append(childrenPb, &pbios.StackChildView{
			ScreenId: int64(id),
		})
	}

	var titleTextStyle *pbtext.TextStyle
	if v.TitleStyle != nil {
		titleTextStyle = v.TitleStyle.MarshalProtobuf()
	}

	var backTextStyle *pbtext.TextStyle
	if v.BackStyle != nil {
		backTextStyle = v.BackStyle.MarshalProtobuf()
	}

	return view.Model{
		Children:       l.Views(),
		Layouter:       l,
		NativeViewName: "gomatcha.io/matcha/view/stacknav",
		NativeViewState: internal.MarshalProtobuf(&pbios.StackView{
			Children:       childrenPb,
			TitleTextStyle: titleTextStyle,
			BackTextStyle:  backTextStyle,
			BarColor:       pb.ColorEncode(v.BarColor),
		}),
		NativeFuncs: map[string]interface{}{
			"OnChange": func(data []byte) {
				pbevent := &pbios.StackEvent{}
				err := proto.Unmarshal(data, pbevent)
				if err != nil {
					fmt.Println("error", err)
					return
				}

				v.Stack.setChildIds(pbevent.Id)
			},
		},
	}
}

type stackBarView struct {
	view.Embed
	Bar *StackBar
}

func (v *stackBarView) Build(ctx view.Context) view.Model {
	l := &constraint.Layouter{}

	// iOS does the layouting for us. We just need the correct sizes.
	hasTitleView := false
	if v.Bar.TitleView != nil {
		hasTitleView = true
		l.Add(v.Bar.TitleView, func(s *constraint.Solver) {
			s.Top(0)
			s.Left(0)
			s.HeightLess(l.MaxGuide().Height())
			s.WidthLess(l.MaxGuide().Width())
		})
	}

	rightViewCount := int64(0)
	for _, i := range v.Bar.RightViews {
		rightViewCount += 1
		l.Add(i, func(s *constraint.Solver) {
			s.Top(0)
			s.Left(0)
			s.HeightLess(l.MaxGuide().Height())
			s.WidthLess(l.MaxGuide().Width())
		})
	}
	leftViewCount := int64(0)
	for _, i := range v.Bar.LeftViews {
		leftViewCount += 1
		l.Add(i, func(s *constraint.Solver) {
			s.Top(0)
			s.Left(0)
			s.HeightLess(l.MaxGuide().Height())
			s.WidthLess(l.MaxGuide().Width())
		})
	}

	return view.Model{
		Layouter:       l,
		Children:       l.Views(),
		NativeViewName: "gomatcha.io/matcha/view/stacknav Bar",
		NativeViewState: internal.MarshalProtobuf(&pbios.StackBar{
			Title: v.Bar.Title,
			CustomBackButtonTitle: len(v.Bar.BackButtonTitle) > 0,
			BackButtonTitle:       v.Bar.BackButtonTitle,
			BackButtonHidden:      v.Bar.BackButtonHidden,
			HasTitleView:          hasTitleView,
			RightViewCount:        rightViewCount,
			LeftViewCount:         leftViewCount,
		}),
	}
}

type StackBar struct {
	Title            string
	BackButtonTitle  string
	BackButtonHidden bool

	TitleView  view.View
	RightViews []view.View
	LeftViews  []view.View
}

func (t *StackBar) OptionKey() string {
	return "gomatcha.io/view/ios StackBar"
}
