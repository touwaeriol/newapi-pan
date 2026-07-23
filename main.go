package main

import (
	"log"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	st, err := openStore(cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.close()
	initialPassword, err := st.ensureAdmin(cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		log.Fatal(err)
	}
	if initialPassword != "" {
		log.Printf("初始管理员：%s / %s（请登录后通过管理员接口修改）", cfg.AdminUsername, initialPassword)
	}
	if err := newServer(cfg, st).listenAndServe(); err != nil {
		log.Fatal(err)
	}
}
