package model

// Server is a saved panel address — a "launcher" entry for another panel
// (this fork, 3x-ui, anything). Clicking it just opens the URL in a new tab.
type Server struct {
	Id       uint   `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Name     string `json:"name" form:"name"`
	Url      string `json:"url" form:"url"`
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`
	Remark   string `json:"remark" form:"remark"`
}
