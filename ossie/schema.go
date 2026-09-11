package ossie

// SchemaSource is the canonical upstream Apache Ossie schema used to generate
// the Go binding in model.go. Metis must not introduce fields that are absent
// from this schema.
const SchemaSource = "https://raw.githubusercontent.com/apache/ossie/88e0011148283302c9a04cd0287e00e0b9d87354/core-spec/osi-schema.json"

// SchemaRepositoryPath documents the authoritative file in Apache Ossie.
const SchemaRepositoryPath = "core-spec/osi-schema.json"

// BindingSpecVersion is the Ossie spec version represented by the checked-in
// generated Go binding. It must stay equal to SupportedSpecVersion.
const BindingSpecVersion = "0.2.0.dev0"
