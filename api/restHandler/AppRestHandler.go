/*
 * Copyright (c) 2020 Devtron Labs
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package restHandler

import (
	"encoding/json"
	"errors"
	"fmt"
	appBean "github.com/devtron-labs/devtron/api/appbean"
	"github.com/devtron-labs/devtron/internal/sql/repository/pipelineConfig"
	"github.com/devtron-labs/devtron/pkg/app"
	"github.com/devtron-labs/devtron/pkg/appWorkflow"
	"github.com/devtron-labs/devtron/pkg/bean"
	"github.com/devtron-labs/devtron/pkg/pipeline"
	"github.com/devtron-labs/devtron/pkg/user"
	"github.com/devtron-labs/devtron/util"
	"github.com/devtron-labs/devtron/util/rbac"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"gopkg.in/go-playground/validator.v9"
	"net/http"
	"strconv"
)

type AppRestHandler interface {
	GetAppAllDetail(w http.ResponseWriter, r *http.Request)
	//CreateApp(w http.ResponseWriter, r *http.Request)
}

type AppRestHandlerImpl struct {
	logger                  *zap.SugaredLogger
	userAuthService         user.UserService
	validator               *validator.Validate
	enforcerUtil            rbac.EnforcerUtil
	enforcer                rbac.Enforcer
	appLabelService         app.AppLabelService
	pipelineBuilder         pipeline.PipelineBuilder
	gitRegistryService      pipeline.GitRegistryConfig
	chartService            pipeline.ChartService
	configMapService        pipeline.ConfigMapService
	appListingService       app.AppListingService
	propertiesConfigService pipeline.PropertiesConfigService
	appWorkflowService      appWorkflow.AppWorkflowService
	materialRepository      pipelineConfig.MaterialRepository
}

func NewAppRestHandlerImpl(logger *zap.SugaredLogger, userAuthService user.UserService, validator *validator.Validate, enforcerUtil rbac.EnforcerUtil,
	enforcer rbac.Enforcer, appLabelService app.AppLabelService, pipelineBuilder pipeline.PipelineBuilder, gitRegistryService pipeline.GitRegistryConfig,
	chartService pipeline.ChartService, configMapService pipeline.ConfigMapService, appListingService app.AppListingService, propertiesConfigService pipeline.PropertiesConfigService, appWorkflowService appWorkflow.AppWorkflowService, materialRepository pipelineConfig.MaterialRepository) *AppRestHandlerImpl {
	handler := &AppRestHandlerImpl{
		logger:                  logger,
		userAuthService:         userAuthService,
		validator:               validator,
		enforcerUtil:            enforcerUtil,
		enforcer:                enforcer,
		appLabelService:         appLabelService,
		pipelineBuilder:         pipelineBuilder,
		gitRegistryService:      gitRegistryService,
		chartService:            chartService,
		configMapService:        configMapService,
		appListingService:       appListingService,
		propertiesConfigService: propertiesConfigService,
		appWorkflowService:      appWorkflowService,
		materialRepository:      materialRepository,
	}
	return handler
}

const (
	CIPIPELINE string = "CI_PIPELINE"
	CDPIPELINE string = "CD_PIPELINE"
)

func (handler AppRestHandlerImpl) GetAppAllDetail(w http.ResponseWriter, r *http.Request) {

	userId, err := handler.userAuthService.GetLoggedInUser(r)
	if userId == 0 || err != nil {
		writeJsonResp(w, err, "Unauthorized User", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	appId, err := strconv.Atoi(vars["appId"])
	if err != nil {
		handler.logger.Errorw("request err, GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return
	}

	//rback implementation for app (user should be admin)
	token := r.Header.Get("token")
	object := handler.enforcerUtil.GetAppRBACNameByAppId(appId)
	if ok := handler.enforcer.Enforce(token, rbac.ResourceApplications, rbac.ActionUpdate, object); !ok {
		handler.logger.Errorw("Unauthorized User for app update action", "err", err, "appId", appId)
		writeJsonResp(w, err, "Unauthorized User", http.StatusForbidden)
		return
	}
	//rback implementation ends here for app

	// get/build app metadata starts
	appMetadataResp, done := handler.validateAndBuildAppMetadata(w, appId)
	if done {
		return
	}
	// get/build app metadata ends

	// get/build git materials starts
	gitMaterialsResp, done := handler.validateAndBuildAppGitMaterials(w, appId)
	if done {
		return
	}
	// get/build git materials ends

	// get/build docker config starts
	dockerConfig, done := handler.validateAndBuildDockerConfig(w, appId)
	if done {
		return
	}
	// get/build docker config ends

	// get/build global deployment template starts
	globalDeploymentTemplateResp, done := handler.validateAndBuildAppDeploymentTemplate(w, appId, 0)
	if done {
		return
	}
	// get/build global deployment template ends

	// get/build app workflows starts
	//appWorkflows, done := handler.validateAndBuildAppWorkflows(w, appId)
	if done {
		return
	}
	// get/build app workflows ends

	// get/build global config maps starts
	globalConfigMapsResp, done := handler.validateAndBuildAppGlobalConfigMaps(w, appId)
	if done {
		return
	}
	// get/build global config maps ends

	// get/build global secrets starts
	globalSecretsResp, done := handler.validateAndBuildAppGlobalSecrets(w, appId)
	if done {
		return
	}
	// get/build global secrets ends

	// get/build environment override starts
	environmentOverrides, done := handler.validateAndBuildEnvironmentOverrides(w, appId, token)
	if done {
		return
	}
	// get/build environment override ends

	// build full object for response
	appDetail := &appBean.AppDetail{
		Metadata:                 appMetadataResp,
		GitMaterials:             gitMaterialsResp,
		DockerConfig:             dockerConfig,
		GlobalDeploymentTemplate: globalDeploymentTemplateResp,
		//AppWorkflows:             appWorkflows,

		GlobalConfigMaps:     globalConfigMapsResp,
		GlobalSecrets:        globalSecretsResp,
		EnvironmentOverrides: environmentOverrides,
	}
	// end

	writeJsonResp(w, nil, appDetail, http.StatusOK)
}

//get/build app metadata
func (handler AppRestHandlerImpl) validateAndBuildAppMetadata(w http.ResponseWriter, appId int) (*appBean.AppMetadata, bool) {
	appMetaInfo, err := handler.appLabelService.GetAppMetaInfo(appId)
	if err != nil {
		handler.logger.Errorw("service err, GetAppMetaInfo in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	if appMetaInfo == nil {
		err = errors.New("invalid appId - appMetaInfo is null")
		handler.logger.Errorw("Validation error ", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return nil, true
	}

	var appLabelsRes []*appBean.AppLabel
	if len(appMetaInfo.Labels) > 0 {
		for _, label := range appMetaInfo.Labels {
			appLabelsRes = append(appLabelsRes, &appBean.AppLabel{
				Key:   label.Key,
				Value: label.Value,
			})
		}
	}
	appMetadataResp := &appBean.AppMetadata{
		AppName:     appMetaInfo.AppName,
		ProjectName: appMetaInfo.ProjectName,
		Labels:      appLabelsRes,
	}

	return appMetadataResp, false
}

//get/build git materials
func (handler AppRestHandlerImpl) validateAndBuildAppGitMaterials(w http.ResponseWriter, appId int) ([]*appBean.GitMaterial, bool) {
	gitMaterials := handler.pipelineBuilder.GetMaterialsForAppId(appId)
	var gitMaterialsResp []*appBean.GitMaterial
	if len(gitMaterials) > 0 {
		for _, gitMaterial := range gitMaterials {
			gitRegistry, err := handler.gitRegistryService.FetchOneGitProvider(strconv.Itoa(gitMaterial.GitProviderId))
			if err != nil {
				handler.logger.Errorw("service err, getGitProvider in GetAppAllDetail", "err", err, "appId", appId)
				writeJsonResp(w, err, nil, http.StatusInternalServerError)
				return nil, true
			}

			gitMaterialsResp = append(gitMaterialsResp, &appBean.GitMaterial{
				GitRepoUrl:      gitMaterial.Url,
				CheckoutPath:    gitMaterial.CheckoutPath,
				FetchSubmodules: gitMaterial.FetchSubmodules,
				GitProviderUrl:  gitRegistry.Url,
			})
		}
	}
	return gitMaterialsResp, false
}

//get/build docker build config
func (handler AppRestHandlerImpl) validateAndBuildDockerConfig(w http.ResponseWriter, appId int) (*appBean.DockerConfig, bool) {
	ciConfig, err := handler.pipelineBuilder.GetCiPipeline(appId)
	if err != nil {
		handler.logger.Errorw("service err, GetCiPipeline in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	//getting gitMaterialUrl by id
	gitMaterial, err := handler.materialRepository.FindById(ciConfig.DockerBuildConfig.GitMaterialId)
	if err != nil {
		handler.logger.Errorw("error in fetching materialUrl by ID in GetAppAllDetail", "err", err, "gitMaterialId", ciConfig.DockerBuildConfig.GitMaterialId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	dockerConfig := &appBean.DockerConfig{
		DockerRegistry:   ciConfig.DockerRegistry,
		DockerRepository: ciConfig.DockerRepository,
		BuildConfig: &appBean.DockerBuildConfig{
			Args:                   ciConfig.DockerBuildConfig.Args,
			DockerfileRelativePath: ciConfig.DockerBuildConfig.DockerfilePath,
			GitMaterialUrl:         gitMaterial.Url,
		},
	}

	return dockerConfig, false
}


//get/build deployment template
func (handler AppRestHandlerImpl) validateAndBuildAppDeploymentTemplate(w http.ResponseWriter, appId int, envId int) (*appBean.DeploymentTemplate, bool) {
	chartRefData, err := handler.chartService.ChartRefAutocompleteForAppOrEnv(appId, envId)
	if err != nil {
		handler.logger.Errorw("service err, ChartRefAutocompleteForAppOrEnv in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	if chartRefData == nil {
		err = errors.New("invalid appId/envId - chartRefData is null")
		handler.logger.Errorw("Validation error ", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return nil, true
	}

	appDeploymentTemplate, err := handler.chartService.FindLatestChartForAppByAppId(appId)
	if err != nil {
		handler.logger.Errorw("service err, GetDeploymentTemplate in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	if appDeploymentTemplate == nil {
		err = errors.New("invalid appId - deploymentTemplate is null")
		handler.logger.Errorw("Validation error ", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusBadRequest)
		return nil, true
	}

	// set deployment template & showAppMetrics
	var showAppMetrics bool
	var deploymentTemplateRaw json.RawMessage
	var chartRefId int
	if envId > 0 {
		// on env level
		env, err := handler.propertiesConfigService.GetEnvironmentProperties(appId, envId, chartRefData.LatestEnvChartRef)
		if err != nil {
			handler.logger.Errorw("service err, GetEnvironmentProperties in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
			writeJsonResp(w, err, nil, http.StatusInternalServerError)
			return nil, true
		}
		chartRefId = chartRefData.LatestEnvChartRef
		if env.EnvironmentConfig.IsOverride {
			deploymentTemplateRaw = env.EnvironmentConfig.EnvOverrideValues
			showAppMetrics = *env.AppMetrics
		} else {
			showAppMetrics = appDeploymentTemplate.IsAppMetricsEnabled
			deploymentTemplateRaw = appDeploymentTemplate.DefaultAppOverride
		}
	} else {
		// on app level
		showAppMetrics = appDeploymentTemplate.IsAppMetricsEnabled
		deploymentTemplateRaw = appDeploymentTemplate.DefaultAppOverride
		chartRefId = chartRefData.LatestAppChartRef
	}

	var deploymentTemplateObj map[string]interface{}
	if deploymentTemplateRaw != nil {
		err = json.Unmarshal([]byte(deploymentTemplateRaw), &deploymentTemplateObj)
		if err != nil {
			handler.logger.Errorw("service err, un-marshling fail in deploymentTemplate", "err", err, "appId", appId)
			writeJsonResp(w, err, nil, http.StatusInternalServerError)
			return nil, true
		}
	}

	deploymentTemplateResp := &appBean.DeploymentTemplate{
		ChartRefId:     chartRefId,
		Template:       deploymentTemplateObj,
		ShowAppMetrics: showAppMetrics,
	}

	return deploymentTemplateResp, false
}

// get/build global config maps
func (handler AppRestHandlerImpl) validateAndBuildAppGlobalConfigMaps(w http.ResponseWriter, appId int) ([]*appBean.ConfigMap, bool) {
	configMapData, err := handler.configMapService.CMGlobalFetch(appId)
	if err != nil {
		handler.logger.Errorw("service err, CMGlobalFetch in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	return handler.validateAndBuildAppConfigMaps(w, appId, 0, configMapData)
}

// get/build environment config maps
func (handler AppRestHandlerImpl) validateAndBuildAppEnvironmentConfigMaps(w http.ResponseWriter, appId int, envId int) ([]*appBean.ConfigMap, bool) {
	configMapData, err := handler.configMapService.CMEnvironmentFetch(appId, envId)
	if err != nil {
		handler.logger.Errorw("service err, CMGlobalFetch in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	return handler.validateAndBuildAppConfigMaps(w, appId, envId, configMapData)
}

// get/build config maps
func (handler AppRestHandlerImpl) validateAndBuildAppConfigMaps(w http.ResponseWriter, appId int, envId int, configMapData *pipeline.ConfigDataRequest) ([]*appBean.ConfigMap, bool) {
	var configMapsResp []*appBean.ConfigMap
	if configMapData != nil && len(configMapData.ConfigData) > 0 {
		for _, configMap := range configMapData.ConfigData {

			// initialise
			configMapRes := &appBean.ConfigMap{
				Name:       configMap.Name,
				IsExternal: configMap.External,
				UsageType:  configMap.Type,
			}

			considerGlobalDefaultData := envId > 0 && configMap.Data == nil

			// set data
			var data json.RawMessage
			if considerGlobalDefaultData {
				data = configMap.DefaultData
			} else {
				data = configMap.Data
			}
			var dataObj map[string]interface{}
			if data != nil {
				err := json.Unmarshal([]byte(data), &dataObj)
				if err != nil {
					handler.logger.Errorw("service err, un-marshling fail in config map", "err", err, "appId", appId)
					writeJsonResp(w, err, nil, http.StatusInternalServerError)
					return nil, true
				}
			}
			configMapRes.Data = dataObj

			// set data volume usage type
			if configMap.Type == util.ConfigMapSecretUsageTypeVolume {
				dataVolumeUsageConfig := &appBean.ConfigMapSecretDataVolumeUsageConfig{
					FilePermission: configMap.FilePermission,
					SubPath:        configMap.SubPath,
				}
				if considerGlobalDefaultData {
					dataVolumeUsageConfig.MountPath = configMap.DefaultMountPath
				} else {
					dataVolumeUsageConfig.MountPath = configMap.MountPath
				}

				configMapRes.DataVolumeUsageConfig = dataVolumeUsageConfig
			}

			configMapsResp = append(configMapsResp, configMapRes)
		}
	}
	return configMapsResp, false
}

// get/build global secrets
func (handler AppRestHandlerImpl) validateAndBuildAppGlobalSecrets(w http.ResponseWriter, appId int) ([]*appBean.Secret, bool) {
	secretData, err := handler.configMapService.CSGlobalFetch(appId)
	if err != nil {
		handler.logger.Errorw("service err, CSGlobalFetch in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	var secretsResp []*appBean.Secret
	if secretData != nil && len(secretData.ConfigData) > 0 {

		for _, secretConfig := range secretData.ConfigData {
			secretDataWithData, err := handler.configMapService.CSGlobalFetchForEdit(secretConfig.Name, secretData.Id)
			if err != nil {
				handler.logger.Errorw("service err, CSGlobalFetch-CSGlobalFetchForEdit in GetAppAllDetail", "err", err, "appId", appId)
				writeJsonResp(w, err, nil, http.StatusInternalServerError)
				return nil, true
			}

			secretRes, err := handler.validateAndBuildAppSecrets(w, appId, 0, secretDataWithData)
			if err != nil {
				handler.logger.Errorw("service err, CSGlobalFetch-validateAndBuildAppSecrets in GetAppAllDetail", "err", err, "appId", appId)
				writeJsonResp(w, err, nil, http.StatusInternalServerError)
				return nil, true
			}

			for _, secret := range secretRes {
				secretsResp = append(secretsResp, secret)
			}
		}
	}

	return secretsResp, false
}

// get/build environment secrets
func (handler AppRestHandlerImpl) validateAndBuildAppEnvironmentSecrets(w http.ResponseWriter, appId int, envId int) ([]*appBean.Secret, bool) {
	secretData, err := handler.configMapService.CSEnvironmentFetch(appId, envId)
	if err != nil {
		handler.logger.Errorw("service err, CSEnvironmentFetch in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	var secretsResp []*appBean.Secret
	if secretData != nil && len(secretData.ConfigData) > 0 {

		for _, secretConfig := range secretData.ConfigData {
			secretDataWithData, err := handler.configMapService.CSEnvironmentFetchForEdit(secretConfig.Name, secretData.Id, appId, envId)
			if err != nil {
				handler.logger.Errorw("service err, CSEnvironmentFetchForEdit in GetAppAllDetail", "err", err, "appId", appId, "envId", envId)
				writeJsonResp(w, err, nil, http.StatusInternalServerError)
				return nil, true
			}

			secretRes, err := handler.validateAndBuildAppSecrets(w, appId, envId, secretDataWithData)
			if err != nil {
				handler.logger.Errorw("service err, CSGlobalFetch-validateAndBuildAppSecrets in GetAppAllDetail", "err", err, "appId", appId)
				writeJsonResp(w, err, nil, http.StatusInternalServerError)
				return nil, true
			}

			for _, secret := range secretRes {
				secretsResp = append(secretsResp, secret)
			}
		}
	}

	return secretsResp, false
}

// get/build secrets
func (handler AppRestHandlerImpl) validateAndBuildAppSecrets(w http.ResponseWriter, appId int, envId int, secretData *pipeline.ConfigDataRequest) ([]*appBean.Secret, error) {
	var secretsResp []*appBean.Secret
	if secretData != nil && len(secretData.ConfigData) > 0 {
		for _, secret := range secretData.ConfigData {

			// initialise
			globalSecret := &appBean.Secret{
				Name:         secret.Name,
				RoleArn:      secret.RoleARN,
				IsExternal:   secret.External,
				UsageType:    secret.Type,
				ExternalType: secret.ExternalSecretType,
			}

			considerGlobalDefaultData := envId > 0 && secret.Data == nil

			// set data
			var data json.RawMessage
			var externalSecrets []pipeline.ExternalSecret
			if considerGlobalDefaultData {
				data = secret.DefaultData
				externalSecrets = secret.DefaultExternalSecret
			} else {
				data = secret.Data
				externalSecrets = secret.ExternalSecret
			}
			var dataObj map[string]interface{}
			if data != nil {
				err := json.Unmarshal([]byte(data), &dataObj)
				if err != nil {
					handler.logger.Errorw("service err, un-marshling fail in secret", "err", err, "appId", appId)
					writeJsonResp(w, err, nil, http.StatusInternalServerError)
					return nil, err
				}
			}
			globalSecret.Data = dataObj

			// set external data
			var externalSecretsResp []*appBean.ExternalSecret
			if len(externalSecrets) > 0 {
				for _, externalSecret := range externalSecrets {
					externalSecretsResp = append(externalSecretsResp, &appBean.ExternalSecret{
						Name:     externalSecret.Name,
						Key:      externalSecret.Key,
						Property: externalSecret.Property,
						IsBinary: externalSecret.IsBinary,
					})
				}
			}
			globalSecret.ExternalSecretData = externalSecretsResp

			// set data volume usage type
			if secret.Type == util.ConfigMapSecretUsageTypeVolume {
				globalSecret.DataVolumeUsageConfig = &appBean.ConfigMapSecretDataVolumeUsageConfig{
					SubPath:        secret.SubPath,
					FilePermission: secret.FilePermission,
				}
				if considerGlobalDefaultData {
					globalSecret.DataVolumeUsageConfig.MountPath = secret.DefaultMountPath
				} else {
					globalSecret.DataVolumeUsageConfig.MountPath = secret.MountPath
				}
			}

			secretsResp = append(secretsResp, globalSecret)
		}
	}
	return secretsResp, nil
}

func (handler AppRestHandlerImpl) validateAndBuildEnvironmentOverrides(w http.ResponseWriter, appId int, token string) (map[string]*appBean.EnvironmentOverride, bool) {
	appEnvironments, err := handler.appListingService.FetchOtherEnvironment(appId)
	if err != nil {
		handler.logger.Errorw("service err, Fetch app environments in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	environmentOverrides := make(map[string]*appBean.EnvironmentOverride)
	if len(appEnvironments) > 0 {
		for _, appEnvironment := range appEnvironments {

			envId := appEnvironment.EnvironmentId

			// check RBAC for environment
			object := handler.enforcerUtil.GetEnvRBACNameByAppId(appId, envId)
			if ok := handler.enforcer.Enforce(token, rbac.ResourceEnvironment, rbac.ActionUpdate, object); !ok {
				handler.logger.Errorw("Unauthorized User for env update action", "err", err, "appId", appId, "envId", envId)
				writeJsonResp(w, fmt.Errorf("unauthorized user"), nil, http.StatusForbidden)
				return nil, true
			}
			// RBAC end

			envDeploymentTemplateResp, done := handler.validateAndBuildAppDeploymentTemplate(w, appId, envId)
			if done {
				return nil, true
			}

			envSecretsResp, done := handler.validateAndBuildAppEnvironmentSecrets(w, appId, envId)
			if done {
				return nil, true
			}

			envConfigMapsResp, done := handler.validateAndBuildAppEnvironmentConfigMaps(w, appId, envId)
			if done {
				return nil, true
			}

			environmentOverrides[appEnvironment.EnvironmentName] = &appBean.EnvironmentOverride{
				Secrets:            envSecretsResp,
				ConfigMaps:         envConfigMapsResp,
				DeploymentTemplate: envDeploymentTemplateResp,
			}
		}
	}
	return environmentOverrides, false
}


func (handler AppRestHandlerImpl) validateAndBuildAppWorkflows(w http.ResponseWriter, appId int) ([]*appBean.AppWorkflow, bool) {
	var appWorkflowsResp []*appBean.AppWorkflow
	workflowsList, err := handler.appWorkflowService.FindAppWorkflows(appId)
	if err != nil {
		handler.logger.Errorw("error in fetching workflows for app in GetAppAllDetail", "err", err)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}
	ciConfig, err := handler.pipelineBuilder.GetCiPipeline(appId)
	if err != nil {
		handler.logger.Errorw("service err, GetCiPipeline in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}
	cdConfig, err := handler.pipelineBuilder.GetCdPipelinesForApp(appId)
	if err != nil {
		handler.logger.Errorw("service err, GetCdPipelines in GetAppAllDetail", "err", err, "appId", appId)
		writeJsonResp(w, err, nil, http.StatusInternalServerError)
		return nil, true
	}

	for _, workflow := range workflowsList {
		workflowResp := &appBean.AppWorkflow{
			Name: workflow.Name,
		}
		var HasInternalCiPipeline bool
		if len(workflow.AppWorkflowMappingDto) == 0 {
			handler.logger.Infow("no pipeline found in workflow in GetAppAllDetail", "workflow", workflow.Name, "appId", appId)
		} else {
			for _, appWorkflowMap := range workflow.AppWorkflowMappingDto {
				if appWorkflowMap.Type == CIPIPELINE {
					for _, ciPipeline := range ciConfig.CiPipelines {
						if ciPipeline.Id == appWorkflowMap.ComponentId {
							//checking if ci pipeline is external or not; if external, will not include this workflow
							if !ciPipeline.IsExternal {
								HasInternalCiPipeline = true
								workflowResp.CiPipeline = BuildCiPipelineResp(ciPipeline)
							}
						}
					}
				} else if appWorkflowMap.Type == CDPIPELINE && HasInternalCiPipeline {
					for _, cdPipeline := range cdConfig.Pipelines {
						if cdPipeline.Id == appWorkflowMap.ComponentId {
							workflowResp.CdPipeline = append(workflowResp.CdPipeline, BuildCdPipelineResp(cdPipeline))
						}
					}
				}
			}
		}
		appWorkflowsResp = append(appWorkflowsResp, workflowResp)
	}
	return appWorkflowsResp, false
}

func BuildCiPipelineResp(ciPipeline *bean.CiPipeline) *appBean.CiPipelineDetails {
	var ciMaterialsResp []*appBean.CiMaterial
	var beforeDockerBuildScriptsResp []*appBean.CiScript
	var afterDockerBuildScriptsResp []*appBean.CiScript
	var beforeDockerBuildTasks []*appBean.Task
	var afterDockerBuildTasks []*appBean.Task
	for _, beforeDockerBuildScript := range ciPipeline.BeforeDockerBuildScripts {
		beforeDockerBuildScriptResp := &appBean.CiScript{
			Name:           beforeDockerBuildScript.Name,
			Index:          beforeDockerBuildScript.Index,
			Script:         beforeDockerBuildScript.Script,
			OutputLocation: beforeDockerBuildScript.OutputLocation,
		}
		beforeDockerBuildScriptsResp = append(beforeDockerBuildScriptsResp, beforeDockerBuildScriptResp)
	}
	for _, afterDockerBuildScript := range ciPipeline.AfterDockerBuildScripts {
		afterDockerBuildScriptResp := &appBean.CiScript{
			Name:           afterDockerBuildScript.Name,
			Index:          afterDockerBuildScript.Index,
			Script:         afterDockerBuildScript.Script,
			OutputLocation: afterDockerBuildScript.OutputLocation,
		}
		afterDockerBuildScriptsResp = append(afterDockerBuildScriptsResp, afterDockerBuildScriptResp)
	}
	for _, ciMaterial := range ciPipeline.CiMaterial {
		ciMaterialResp := &appBean.CiMaterial{
			Path:            ciMaterial.Path,
			CheckoutPath:    ciMaterial.CheckoutPath,
			GitMaterialName: ciMaterial.GitMaterialName,
		}
		ciMaterialResp.Source = &appBean.SourceTypeConfig{
			Type:  ciMaterial.Source.Type,
			Value: ciMaterial.Source.Value,
		}
		ciMaterialsResp = append(ciMaterialsResp, ciMaterialResp)
	}
	for _, beforeDockerBuild := range ciPipeline.BeforeDockerBuild {
		beforeDockerBuildTask := &appBean.Task{
			Name: beforeDockerBuild.Name,
			Type: beforeDockerBuild.Type,
			Cmd:  beforeDockerBuild.Cmd,
			Args: beforeDockerBuild.Args,
		}
		beforeDockerBuildTasks = append(beforeDockerBuildTasks, beforeDockerBuildTask)
	}
	for _, afterDockerBuild := range ciPipeline.AfterDockerBuild {
		afterDockerBuildTask := &appBean.Task{
			Name: afterDockerBuild.Name,
			Type: afterDockerBuild.Type,
			Cmd:  afterDockerBuild.Cmd,
			Args: afterDockerBuild.Args,
		}
		afterDockerBuildTasks = append(afterDockerBuildTasks, afterDockerBuildTask)
	}

	ciPipelineResp := &appBean.CiPipelineDetails{
		Name:                     ciPipeline.Name,
		IsManual:                 ciPipeline.IsManual,
		DockerArgs:               ciPipeline.DockerArgs,
		LinkedCount:              ciPipeline.LinkedCount,
		ScanEnabled:              ciPipeline.ScanEnabled,
		CiMaterials:              ciMaterialsResp,
		BeforeDockerBuildScripts: beforeDockerBuildScriptsResp,
		AfterDockerBuildScripts:  afterDockerBuildScriptsResp,
		BeforeDockerBuild:        beforeDockerBuildTasks,
		AfterDockerBuild:         afterDockerBuildTasks,
	}
	return ciPipelineResp
}

func BuildCdPipelineResp(cdPipeline *bean.CDPipelineConfigObject) *appBean.CdPipelineDetails {
	var strategiesResp []appBean.Strategy
	for _, strategy := range cdPipeline.Strategies {
		strategyResp := appBean.Strategy{
			DeploymentTemplate: strategy.DeploymentTemplate,
			Config:             strategy.Config,
			Default:            strategy.Default,
		}
		strategiesResp = append(strategiesResp, strategyResp)
	}
	preStagesResp := appBean.CdStage{
		TriggerType: cdPipeline.PreStage.TriggerType,
		Name:        cdPipeline.PreStage.Name,
		Config:      cdPipeline.PreStage.Config,
		Status:      cdPipeline.PreStage.Status,
	}
	postStagesResp := appBean.CdStage{
		TriggerType: cdPipeline.PostStage.TriggerType,
		Name:        cdPipeline.PostStage.Name,
		Config:      cdPipeline.PostStage.Config,
		Status:      cdPipeline.PostStage.Status,
	}
	preStageConfigMapSecretNamesResp := appBean.PreStageConfigMapSecretNames{
		ConfigMaps: cdPipeline.PreStageConfigMapSecretNames.ConfigMaps,
		Secrets:    cdPipeline.PreStageConfigMapSecretNames.Secrets,
	}
	postStageConfigMapSecretNamesResp := appBean.PostStageConfigMapSecretNames{
		ConfigMaps: cdPipeline.PostStageConfigMapSecretNames.ConfigMaps,
		Secrets:    cdPipeline.PostStageConfigMapSecretNames.Secrets,
	}

	cdPipelineResp := &appBean.CdPipelineDetails{
		EnvironmentName:               cdPipeline.EnvironmentName,
		TriggerType:                   cdPipeline.TriggerType,
		Name:                          cdPipeline.Name,
		Strategies:                    strategiesResp,
		Namespace:                     cdPipeline.Namespace,
		DeploymentTemplate:            cdPipeline.DeploymentTemplate,
		PreStage:                      preStagesResp,
		PostStage:                     postStagesResp,
		PreStageConfigMapSecretNames:  preStageConfigMapSecretNamesResp,
		PostStageConfigMapSecretNames: postStageConfigMapSecretNamesResp,
		RunPreStageInEnv:              cdPipeline.RunPreStageInEnv,
		RunPostStageInEnv:             cdPipeline.RunPostStageInEnv,
		CdArgoSetup:                   cdPipeline.CdArgoSetup,
	}
	return cdPipelineResp
}
