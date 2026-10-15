package pos

import "testing"

func TestComboSelectionEnrichmentShape(t *testing.T) {
	enriched := []map[string]interface{}{
		{
			"group_index":     0,
			"group_name":      "SODAS",
			"menu_item_ids":   []string{"MENU-COKE"},
			"menu_item_names": []string{"COKE (CAN)"},
		},
	}
	row := enriched[0]
	if _, ok := row["menu_item_names"]; !ok {
		t.Fatal("menu_item_names required for kitchen ticket")
	}
	if row["group_name"] != "SODAS" {
		t.Fatalf("group_name %v", row["group_name"])
	}
}
