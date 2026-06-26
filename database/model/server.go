package model

// Server is a saved panel address — a "launcher" entry for another panel
// (this fork, 3x-ui, anything). Clicking it just opens the URL in a new tab.
type Server struct {
	Id   uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Name string `json:"name" form:"name"`
	Url  string `json:"url" form:"url"`
	// Token is the remote s-ui APIv2 token used for central management.
	Token  string `json:"token" form:"token"`
	Remark string `json:"remark" form:"remark"`
}
