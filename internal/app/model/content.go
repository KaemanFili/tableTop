package model

type ContentSource struct {
	PluginID string `json:"pluginID" bson:"pluginID"`
	Version  int    `json:"version" bson:"version"`
	Key      string `json:"key" bson:"key"`
}
