package ossie

import "testing"

func TestTemporalRelationshipContract(t *testing.T) {
	isTime := true
	model := &SemanticModel{
		Name: "commerce",
		Datasets: []Dataset{
			{Name: "orders", Source: "analytics.orders", Fields: []Field{
				{Name: "customer_id", Datatype: DataTypeString, Expression: testExpression("orders.customer_id")},
				{Name: "order_date", Datatype: DataTypeDate, Expression: testExpression("orders.order_date"), Dimension: &Dimension{IsTime: &isTime}},
			}},
			{Name: "customer_history", Source: "analytics.customer_history", Fields: []Field{
				{Name: "customer_id", Datatype: DataTypeString, Expression: testExpression("customer_history.customer_id")},
				{Name: "valid_from", Datatype: DataTypeDateTime, Expression: testExpression("customer_history.valid_from")},
				{Name: "valid_to", Datatype: DataTypeDateTime, Expression: testExpression("customer_history.valid_to")},
			}},
		},
		Relationships: []Relationship{{
			Name: "orders_to_customer_history", From: "orders", To: "customer_history",
			FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"},
			CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: `{"kind":"temporal","from_time_dimension":"order_date","to_valid_from":"valid_from","to_valid_to":"valid_to","cardinality":"many_to_one"}`}},
		}},
	}
	spec, ok, err := TemporalRelationship(&model.Relationships[0])
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("temporal relationship extension not found")
	}
	if spec.FromTimeDimension != "order_date" || spec.ToValidFrom != "valid_from" || spec.ToValidTo != "valid_to" || spec.Cardinality != TemporalCardinalityManyToOne {
		t.Fatalf("spec = %#v", spec)
	}
	if err := ValidateTemporalRelationshipModel(model, &model.Relationships[0], spec); err != nil {
		t.Fatal(err)
	}
}

func TestTemporalRelationshipRejectsInvalidReferences(t *testing.T) {
	isTime := true
	base := &SemanticModel{Datasets: []Dataset{
		{Name: "orders", Source: "analytics.orders", Fields: []Field{{Name: "order_date", Datatype: DataTypeDate, Expression: testExpression("orders.order_date"), Dimension: &Dimension{IsTime: &isTime}}}},
		{Name: "history", Source: "analytics.history", Fields: []Field{{Name: "valid_from", Datatype: DataTypeDateTime, Expression: testExpression("history.valid_from")}, {Name: "valid_to", Datatype: DataTypeDateTime, Expression: testExpression("history.valid_to")}}},
	}}
	rel := &Relationship{Name: "r", From: "orders", To: "history", FromColumns: []string{"id"}, ToColumns: []string{"id"}}
	cases := map[string]TemporalRelationshipSpec{
		"missing fact time":    {Kind: RelationshipExtensionTemporal, FromTimeDimension: "missing", ToValidFrom: "valid_from", ToValidTo: "valid_to", Cardinality: TemporalCardinalityManyToOne},
		"missing valid from":   {Kind: RelationshipExtensionTemporal, FromTimeDimension: "order_date", ToValidFrom: "missing", ToValidTo: "valid_to", Cardinality: TemporalCardinalityManyToOne},
		"same validity fields": {Kind: RelationshipExtensionTemporal, FromTimeDimension: "order_date", ToValidFrom: "valid_from", ToValidTo: "valid_from", Cardinality: TemporalCardinalityManyToOne},
		"missing cardinality":  {Kind: RelationshipExtensionTemporal, FromTimeDimension: "order_date", ToValidFrom: "valid_from", ToValidTo: "valid_to"},
		"fanout cardinality":   {Kind: RelationshipExtensionTemporal, FromTimeDimension: "order_date", ToValidFrom: "valid_from", ToValidTo: "valid_to", Cardinality: "one_to_many"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTemporalRelationshipSpec(spec); err == nil {
				if err := ValidateTemporalRelationshipModel(base, rel, spec); err == nil {
					t.Fatal("expected validation error")
				}
			}
		})
	}
}

func TestTemporalOneToOneRequiresUniqueFactKey(t *testing.T) {
	isTime := true
	model := &SemanticModel{Datasets: []Dataset{
		{Name: "orders", Source: "analytics.orders", PrimaryKey: []string{"order_id"}, Fields: []Field{
			{Name: "customer_id", Datatype: DataTypeString, Expression: testExpression("orders.customer_id")},
			{Name: "order_date", Datatype: DataTypeDate, Expression: testExpression("orders.order_date"), Dimension: &Dimension{IsTime: &isTime}},
		}},
		{Name: "history", Source: "analytics.history", Fields: []Field{
			{Name: "customer_id", Datatype: DataTypeString, Expression: testExpression("history.customer_id")},
			{Name: "valid_from", Datatype: DataTypeDateTime, Expression: testExpression("history.valid_from")},
			{Name: "valid_to", Datatype: DataTypeDateTime, Expression: testExpression("history.valid_to")},
		}},
	}}
	rel := &Relationship{Name: "r", From: "orders", To: "history", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}}
	spec := TemporalRelationshipSpec{Kind: RelationshipExtensionTemporal, FromTimeDimension: "order_date", ToValidFrom: "valid_from", ToValidTo: "valid_to", Cardinality: TemporalCardinalityOneToOne}
	if err := ValidateTemporalRelationshipModel(model, rel, spec); err == nil {
		t.Fatal("expected one_to_one uniqueness proof failure")
	}
	model.Datasets[0].UniqueKeys = [][]string{{"customer_id"}}
	if err := ValidateTemporalRelationshipModel(model, rel, spec); err != nil {
		t.Fatal(err)
	}
}

func testExpression(sql string) Expression {
	return Expression{Dialects: []DialectExpression{{Dialect: DialectANSISQL, Expression: sql}}}
}
