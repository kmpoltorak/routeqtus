package gateway

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/kmpoltorak/routeqtus/internal/config"
)

func BenchmarkRouter(b *testing.B) {
	for _, n := range []int{1, 100, 1000} {
		s := &Snapshot{}
		for i := 0; i < n; i++ {
			s.routes = append(s.routes, route{Route: config.Route{ID: fmt.Sprintf("r%d", i), Path: fmt.Sprintf("/route/%04d", i)}})
		}
		for _, match := range []bool{true, false} {
			p := "/missing"
			if match {
				p = fmt.Sprintf("/route/%04d", n-1)
			}
			r := httptest.NewRequest("GET", p, nil)
			b.Run(fmt.Sprintf("routes=%d/match=%v", n, match), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					s.match(r)
				}
			})
		}
	}
}
