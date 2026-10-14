package model

type PluginBinding struct {
	PluginID    string `json:"pluginID" bson:"pluginID"`
	Version     int    `json:"version" bson:"version"`
	Fingerprint string `json:"fingerprint" bson:"fingerprint"`
}
type ArtworkReference struct {
	PluginID string `json:"pluginID" bson:"pluginID"`
	Version  int    `json:"version" bson:"version"`
	Kind     string `json:"kind" bson:"kind"`
	ImageID  string `json:"imageID" bson:"imageID"`
}
