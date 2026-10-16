package menu

import "testing"

func TestMenuEventMatchesShop(t *testing.T) {
	cases := []struct {
		name, shop string
		payload    map[string]interface{}
		want       bool
	}{
		{name: "same snake case shop", shop: "shop-a", payload: map[string]interface{}{"shop_id": "shop-a"}, want: true},
		{name: "same camel case shop", shop: "shop-a", payload: map[string]interface{}{"shopId": "shop-a"}, want: true},
		{name: "different shop rejected", shop: "shop-a", payload: map[string]interface{}{"shop_id": "shop-b"}, want: false},
		{name: "missing payload scope allowed only for scoped query", shop: "shop-a", payload: map[string]interface{}{}, want: true},
		{name: "empty requested shop rejected", shop: "", payload: map[string]interface{}{"shop_id": "shop-a"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := menuEventMatchesShop(tc.payload, tc.shop); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
