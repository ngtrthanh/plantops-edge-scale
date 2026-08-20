//go:build !windows

package winservice

import (
	"context"
	"errors"
	"time"
)

const (
	Name        = "PlantOpsEdgeScale"
	DisplayName = "PlantOps Edge Scale"
	Description = "Offline-first PlantOps unmanned truck scale edge controller"
)

type RunFunc func(context.Context) error

func IsService() (bool,error){return false,nil}
func Run(RunFunc) error{return errors.New("Windows Service host is available only on Windows")}
func Install([]string)error{return errors.New("Windows Service management is available only on Windows")}
func Start()error{return errors.New("Windows Service management is available only on Windows")}
func Stop(time.Duration)error{return errors.New("Windows Service management is available only on Windows")}
func Uninstall()error{return errors.New("Windows Service management is available only on Windows")}
func Status()(string,error){return "UNAVAILABLE",errors.New("Windows Service management is available only on Windows")}
func StripManagementArg(args []string)[]string{return append([]string(nil),args...)}
