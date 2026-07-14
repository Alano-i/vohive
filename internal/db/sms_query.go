package db

import "time"

type SIMDeviceAssignment struct {
	ICCID      string `gorm:"column:iccid"`
	DeviceID   string `gorm:"column:device_id"`
	DeviceName string `gorm:"column:device_name"`
}

// GetSIMDeviceAssignments returns the last physical device associated with
// every known SIM/eSIM profile.
func GetSIMDeviceAssignments() ([]SIMDeviceAssignment, error) {
	if DB == nil {
		return []SIMDeviceAssignment{}, nil
	}
	var out []SIMDeviceAssignment
	err := DB.Table("sim_cards AS sc").
		Select("sc.iccid AS iccid, d.alias AS device_id, d.alias AS device_name").
		Joins("JOIN devices AS d ON d.imei = sc.current_imei").
		Where("sc.iccid <> '' AND d.alias <> ''").
		Scan(&out).Error
	return out, err
}

func GetSMSContacts(limit int, beforeTs *time.Time, beforePeer string) ([]SMSContact, error) {
	if DB == nil {
		return []SMSContact{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := DB.Model(&SMSContact{})
	if beforeTs != nil && !beforeTs.IsZero() {
		if beforePeer != "" {
			q = q.Where("last_timestamp < ? OR (last_timestamp = ? AND peer < ?)", *beforeTs, *beforeTs, beforePeer)
		} else {
			q = q.Where("last_timestamp < ?", *beforeTs)
		}
	}

	var out []SMSContact
	err := q.Order("last_timestamp desc, peer desc").Limit(limit).Find(&out).Error
	return out, err
}

func GetSMSContactsByIMSI(imsi string, limit int, beforeTs *time.Time, beforePeer string) ([]SMSContact, error) {
	if DB == nil {
		return []SMSContact{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := DB.Model(&SMSContact{}).Where("imsi = ?", imsi)
	if beforeTs != nil && !beforeTs.IsZero() {
		if beforePeer != "" {
			q = q.Where("last_timestamp < ? OR (last_timestamp = ? AND peer < ?)", *beforeTs, *beforeTs, beforePeer)
		} else {
			q = q.Where("last_timestamp < ?", *beforeTs)
		}
	}

	var out []SMSContact
	err := q.Order("last_timestamp desc, peer desc").Limit(limit).Find(&out).Error
	return out, err
}

func GetSMSContactsByICCID(iccid string, limit int, beforeTs *time.Time, beforePeer string) ([]SMSContact, error) {
	if DB == nil {
		return []SMSContact{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := DB.Model(&SMSContact{}).Where("iccid = ?", iccid)
	if beforeTs != nil && !beforeTs.IsZero() {
		if beforePeer != "" {
			q = q.Where("last_timestamp < ? OR (last_timestamp = ? AND peer < ?)", *beforeTs, *beforeTs, beforePeer)
		} else {
			q = q.Where("last_timestamp < ?", *beforeTs)
		}
	}

	var out []SMSContact
	err := q.Order("last_timestamp desc, peer desc").Limit(limit).Find(&out).Error
	return out, err
}

// GetSMSContactsByDeviceID includes every SIM/eSIM profile previously synced
// on the physical modem, not only the currently active profile.
func GetSMSContactsByDeviceID(deviceID string, limit int, beforeTs *time.Time, beforePeer string) ([]SMSContact, error) {
	if DB == nil {
		return []SMSContact{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	iccids := DB.Table("sim_cards AS sc").
		Select("sc.iccid").
		Joins("JOIN devices AS d ON d.imei = sc.current_imei").
		Where("d.alias = ? OR d.imei = ?", deviceID, deviceID)
	q := DB.Model(&SMSContact{}).Where("iccid IN (?)", iccids)
	if beforeTs != nil && !beforeTs.IsZero() {
		if beforePeer != "" {
			q = q.Where("last_timestamp < ? OR (last_timestamp = ? AND peer < ?)", *beforeTs, *beforeTs, beforePeer)
		} else {
			q = q.Where("last_timestamp < ?", *beforeTs)
		}
	}

	var out []SMSContact
	err := q.Order("last_timestamp desc, peer desc").Limit(limit).Find(&out).Error
	return out, err
}

func GetSMSByIMSIAndPeer(imsi string, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]SMS, error) {
	if DB == nil {
		return []SMS{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := DB.Model(&SMS{}).Where("imsi = ? AND peer = ?", imsi, peer)
	if beforeTs != nil && !beforeTs.IsZero() && beforeID > 0 {
		q = q.Where("timestamp < ? OR (timestamp = ? AND id < ?)", *beforeTs, *beforeTs, beforeID)
	} else if beforeTs != nil && !beforeTs.IsZero() {
		q = q.Where("timestamp < ?", *beforeTs)
	} else if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}

	var out []SMS
	err := q.Order("timestamp desc, id desc").Limit(limit).Find(&out).Error
	return out, err
}

func GetSMSByICCIDAndPeer(iccid string, peer string, limit int, beforeTs *time.Time, beforeID uint) ([]SMS, error) {
	if DB == nil {
		return []SMS{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := DB.Model(&SMS{}).Where("iccid = ? AND peer = ?", iccid, peer)
	if beforeTs != nil && !beforeTs.IsZero() && beforeID > 0 {
		q = q.Where("timestamp < ? OR (timestamp = ? AND id < ?)", *beforeTs, *beforeTs, beforeID)
	} else if beforeTs != nil && !beforeTs.IsZero() {
		q = q.Where("timestamp < ?", *beforeTs)
	} else if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}

	var out []SMS
	err := q.Order("timestamp desc, id desc").Limit(limit).Find(&out).Error
	return out, err
}
