package service

import (
	"encoding/json"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"

	"gorm.io/gorm"
)

// ServerListService manages the saved panel addresses (the server launcher).
// Note: distinct from ServerService (which reports this host's system info).
type ServerListService struct{}

func (s *ServerListService) GetAll() ([]model.Server, error) {
	db := database.GetDB()
	var servers []model.Server
	err := db.Model(model.Server{}).Find(&servers).Error
	if err != nil {
		return nil, err
	}
	return servers, nil
}

func (s *ServerListService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	switch act {
	case "new", "edit":
		var server model.Server
		if err := json.Unmarshal(data, &server); err != nil {
			return err
		}
		return tx.Save(&server).Error
	case "del":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(model.Server{}).Error
	default:
		return common.NewErrorf("unknown action: %s", act)
	}
}
