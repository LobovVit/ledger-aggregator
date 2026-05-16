import './styles.css';

const API_BASE_KEY = 'svap-ui-api-base';
const USER_ID_KEY = 'svap-ui-user-id';

const state = {
  section: 'fsg',
  apiBase: localStorage.getItem(API_BASE_KEY) || '/api/v1',
  userId: localStorage.getItem(USER_ID_KEY) || 'test-user',
  savedQueries: [],
  analyticalAttributes: [],
  dictionaryItems: [],
  queryExecutions: [],
  selectedQueryResults: [],
  selectedQueryIds: {},
  selectedColumnOrders: {},
  selectedHistoryResultId: '',
  selectedResultRows: [],
  selectedResultRowsLoading: false,
  selectedResultRowsError: '',
  executeDataLoaded: false,
  executeDataLoading: false,
};

const staticSections = {
  queries: { id: 'queries', title: 'Запросы', hint: 'Создание, список, получение и удаление сохраненных запросов.' },
  executions: { id: 'executions', title: 'Задачи', hint: 'Статусы запусков: queued, running, succeeded, failed.' },
  results: { id: 'results', title: 'Результаты', hint: 'Список результатов, данные результата и удаление.' },
  dictionaries: { id: 'dictionaries', title: 'Справочники', hint: 'Поиск, upsert, получение и удаление элементов справочников.' },
  config: { id: 'config', title: 'Конфигурация', hint: 'Просмотр и применение динамической конфигурации.' },
  info: { id: 'info', title: 'Info', hint: 'Служебная информация приложения и Swagger.' },
};

function getSections() {
  return [
    staticSections.queries,
    ...configuredQueryTypes().map((type) => ({
      id: type.toLowerCase(),
      title: type,
      hint: `Конструктор, запуск и результаты запросов ${type}.`,
    })),
    staticSections.executions,
    staticSections.results,
    staticSections.dictionaries,
    staticSections.config,
    staticSections.info,
  ];
}

function field(id) {
  return document.getElementById(id);
}

function value(id) {
  return field(id)?.value.trim() ?? '';
}

function checked(id) {
  return Boolean(field(id)?.checked);
}

function dictionaryItemsByCode(dictionaryCode) {
  return state.dictionaryItems.filter((item) => item.dictionary_code === dictionaryCode);
}

function configuredQueryTypes() {
  return dictionaryItemsByCode('SVAP_QUERY_TYPES').map((item) => item.code);
}

function isQueryWorkspaceSection(section = state.section) {
  return configuredQueryTypes()
    .map((type) => type.toLowerCase())
    .includes(section);
}

function activeQueryType() {
  return isQueryWorkspaceSection() ? state.section.toUpperCase() : configuredQueryTypes()[0] || '';
}

function parseJson(raw, emptyValue = {}) {
  if (!raw.trim()) return emptyValue;
  return JSON.parse(raw);
}

function defaultQueryPayloadFromDictionaries() {
  const queryType = configuredQueryTypes()[0] || '';
  return {
    name: `${queryType} запрос`,
    description: '',
    visibility: 'private',
    query_type: queryType,
    params: {},
  };
}

function stringify(data) {
  return JSON.stringify(data, null, 2);
}

function escapeHtml(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function setResult(id, data) {
  const el = field(id);
  if (!el) return;
  el.textContent = typeof data === 'string' ? data : stringify(data);
}

function setStatus(id, message, kind = '') {
  const el = field(id);
  if (!el) return;
  el.className = `status-line ${kind}`;
  el.textContent = message;
}

function normalizeApiBase(base) {
  return (base || '/api/v1').replace(/\/$/, '');
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json');
  }
  if (state.userId) {
    headers.set('X-User-ID', state.userId);
  }

  const response = await fetch(`${normalizeApiBase(state.apiBase)}${path}`, {
    ...options,
    headers,
  });

  const contentType = response.headers.get('content-type') || '';
  const bodyText = await response.text();
  let body = bodyText;
  if (contentType.includes('application/json') && bodyText) {
    body = JSON.parse(bodyText);
  }

  if (!response.ok) {
    const details = typeof body === 'string' ? body.trim() : stringify(body);
    throw new Error(`${response.status} ${response.statusText}${details ? `: ${details}` : ''}`);
  }
  return body || { status: response.status, statusText: response.statusText };
}

async function run(action, resultId, statusId) {
  setStatus(statusId, 'Выполняется...');
  try {
    const data = await action();
    setResult(resultId, data);
    setStatus(statusId, 'Готово', 'ok');
  } catch (error) {
    setResult(resultId, error.message);
    setStatus(statusId, 'Ошибка', 'error');
  }
}

function updateGlobalSettings() {
  state.apiBase = normalizeApiBase(value('apiBase'));
  state.userId = value('userId');
  state.executeDataLoaded = false;
  localStorage.setItem(API_BASE_KEY, state.apiBase);
  localStorage.setItem(USER_ID_KEY, state.userId);
  setStatus('infoSettingsStatus', 'Настройки сохранены', 'ok');
}

function parseQueryParams(query) {
  if (!query?.params) return {};
  if (typeof query.params === 'string') return parseJson(query.params, {});
  return query.params;
}

function activeSavedQueries() {
  const type = activeQueryType();
  return state.savedQueries.filter((query) => query.query_type === type);
}

function selectedQueryId() {
  return state.selectedQueryIds[activeQueryType()] || '';
}

function setSelectedQueryId(id) {
  state.selectedQueryIds = { ...state.selectedQueryIds, [activeQueryType()]: id };
  state.selectedHistoryResultId = '';
  state.selectedResultRows = [];
  state.selectedResultRowsError = '';
}

function selectedSavedQuery() {
  const queries = activeSavedQueries();
  return queries.find((query) => query.id === selectedQueryId()) || queries[0] || null;
}

function attrLabel(code) {
  if (isIndicatorColumn(code)) {
    const item = indicatorItemByColumnCode(code);
    return item ? `${item.short_name || item.full_name || item.code} (${item.code.replace(':', ' DATA_')})` : code;
  }
  return state.analyticalAttributes.find((attr) => attr.code === code)?.name || code;
}

function allAttributeCodes() {
  return state.analyticalAttributes.map((attr) => attr.code);
}

function configuredAttributeCodes(kind) {
  const dictionaryCode = `SVAP_QUERY_${kind}_${activeQueryType()}`;
  const codes = dictionaryItemsByCode(dictionaryCode)
    .map((item) => item.analytical_attribute_code || item.code)
    .filter(Boolean);
  return codes;
}

function configuredColumnCodes() {
  return [...configuredAttributeColumnCodes(), ...configuredIndicatorColumnCodes()];
}

function configuredAttributeColumnCodes() {
  return configuredAttributeCodes('COLUMNS');
}

function configuredFilterCodes() {
  return configuredAttributeCodes('FILTERS');
}

function operationOptions() {
  return dictionaryItemsByCode('SVAP_QUERY_OPERATIONS').map((item) => [item.code, item.short_name || item.full_name || item.code]);
}

function defaultReportType() {
  const queryType = activeQueryType();
  const reportTypes = dictionaryItemsByCode(`SVAP_QUERY_REPORT_TYPES_${queryType}`).map((item) => item.code);
  return reportTypes[0] || '';
}

function reportTypeOptions(selectedValue = defaultReportType()) {
  const queryType = activeQueryType();
  const options = dictionaryItemsByCode(`SVAP_QUERY_REPORT_TYPES_${queryType}`).map((item) => [item.code, item.short_name || item.full_name || item.code]);
  return optionList(options, selectedValue);
}

function configuredIndicatorColumnCodes() {
  return dictionaryItemsByCode(`SVAP_QUERY_INDICATORS_${activeQueryType()}`).map((item) => `indicator:${item.code}`);
}

function isIndicatorColumn(code) {
  return String(code || '').startsWith('indicator:');
}

function indicatorCodeFromColumn(code) {
  return String(code || '').replace(/^indicator:/, '');
}

function indicatorItemByColumnCode(code) {
  const itemCode = indicatorCodeFromColumn(code);
  return state.dictionaryItems.find((item) => item.dictionary_code.startsWith('SVAP_QUERY_INDICATORS_') && item.code === itemCode);
}

function indicatorColumnCodesFromParams(params = {}) {
  const rawIndicators = [];
  if (typeof params.Indicators === 'string') {
    rawIndicators.push(...params.Indicators.split(','));
  }
  if (Array.isArray(params.Indicators)) {
    rawIndicators.push(...params.Indicators);
  }
  if (Array.isArray(params.Groups)) {
    params.Groups.forEach((group) => {
      if (typeof group.indicators === 'string') rawIndicators.push(...group.indicators.split(','));
      if (Array.isArray(group.indicators)) rawIndicators.push(...group.indicators);
    });
  }

  const reportType = params.ReportType || defaultReportType();
  return rawIndicators
    .map((indicator) => String(indicator).trim())
    .filter(Boolean)
    .map((indicator) => (indicator.includes(':') ? `indicator:${indicator}` : `indicator:${reportType}:${indicator}`));
}

function resultMetricColumnCodesFromParams(params = {}) {
  const configured = new Set(configuredIndicatorColumnCodes());
  const raw = Array.isArray(params.ResultParams) ? params.ResultParams : [];
  return raw
    .map((code) => `indicator:${code}`)
    .filter((code) => configured.has(code));
}

function currentVisualParams(params = parseQueryParams(selectedSavedQuery())) {
  if (Array.isArray(params.VisualParams) && params.VisualParams.length > 0) return params.VisualParams;
  return configuredAttributeCodes('COLUMNS').slice(0, 5);
}

function currentColumnCodes(params = parseQueryParams(selectedSavedQuery())) {
  const visualParams = Array.isArray(params.VisualParams) ? params.VisualParams : [];
  const indicatorColumns = ['PA', 'CONS'].includes(activeQueryType())
    ? indicatorColumnCodesFromParams(params)
    : resultMetricColumnCodesFromParams(params);
  if (visualParams.length || indicatorColumns.length) return [...visualParams, ...indicatorColumns];
  const attrColumns = configuredAttributeColumnCodes().slice(0, 3);
  const indicatorDefaults = configuredIndicatorColumnCodes().slice(0, 3);
  return [...attrColumns, ...indicatorDefaults];
}

function dictionaryValuesForAttr(code) {
  return state.dictionaryItems.filter((item) => item.analytical_attribute_code === code && !item.dictionary_code.startsWith('SVAP_QUERY_'));
}

function optionList(options, selectedValue = '') {
  return options
    .map(([value, label]) => `<option value="${escapeHtml(value)}" ${value === selectedValue ? 'selected' : ''}>${escapeHtml(label)}</option>`)
    .join('');
}

function attributeOptions(selectedCode = '', codes = allAttributeCodes()) {
  return codes
    .map((code) => `<option value="${escapeHtml(code)}" ${code === selectedCode ? 'selected' : ''}>${escapeHtml(attrLabel(code))}</option>`)
    .join('');
}

function dictionaryValueOptions(attrCode, selectedValue = '') {
  const items = dictionaryValuesForAttr(attrCode);
  return items
    .map(
      (item) =>
        `<option value="${escapeHtml(item.code)}" ${item.code === selectedValue ? 'selected' : ''}>${escapeHtml(item.short_name || item.code)}</option>`,
    )
    .join('');
}

function selectedColumnCodesFromDOM() {
  return Array.from(document.querySelectorAll('[data-selected-fsg-param]')).map((item) => item.dataset.selectedFsgParam);
}

function columnOrderKey(queryID = selectedQueryId()) {
  return `${activeQueryType()}:${queryID || 'draft'}`;
}

function rememberSelectedColumnOrder() {
  const order = selectedColumnCodesFromDOM();
  state.selectedColumnOrders = { ...state.selectedColumnOrders, [columnOrderKey()]: order };
}

function renderColumnChip(code) {
  return `
    <span class="fsg-column-label" data-selected-fsg-param="${escapeHtml(code)}" draggable="true">
      <span>${escapeHtml(attrLabel(code))}</span>
      <button type="button" data-remove-fsg-param="${escapeHtml(code)}" title="Убрать колонку">×</button>
    </span>
  `;
}

function renderAvailableColumnRow(code) {
  return `
    <button class="fsg-detail-row" type="button" data-add-fsg-param="${escapeHtml(code)}">
      <span>${escapeHtml(attrLabel(code))}</span>
      <span>+</span>
    </button>
  `;
}

function renderAvailableColumnList(codes, emptyText) {
  return codes.map(renderAvailableColumnRow).join('') || `<div class="fsg-empty">${escapeHtml(emptyText)}</div>`;
}

function renderFilterRow(row, params) {
  const selectedCode = row.code;
  const selectedOperation = row.operation || operationOptions()[0]?.[0] || '';
  const selectedValue = row.defaultValue || '';
  return `
    <div class="fsg-filter-row" data-fsg-filter-row>
      <select data-fsg-filter-param>${attributeOptions(selectedCode, configuredFilterCodes())}</select>
      <select data-fsg-filter-operation>${optionList(operationOptions(), selectedOperation)}</select>
      <select id="${escapeHtml(row.id)}" data-fsg-filter-value data-selected-value="${escapeHtml(selectedValue)}">
        ${dictionaryValueOptions(selectedCode, selectedValue)}
      </select>
      <button class="fsg-row-button danger" type="button" data-remove-filter-row title="Удалить условие">×</button>
    </div>
  `;
}

function defaultFilterRowForCode(code, index) {
  return {
    id: `filter-${index}`,
    code,
    defaultValue: '',
    operation: operationOptions()[0]?.[0] || '',
  };
}

function filterRowsForParams(params) {
  if (Array.isArray(params.FilterParams) && params.FilterParams.length > 0) {
    return params.FilterParams.map((filter, index) => ({
      id: `filter-${index}`,
      code: filter.param,
      defaultValue: filter.value || '',
      operation: filter.operation || '=',
    })).filter((row) => row.code);
  }
  if (Array.isArray(params.Groups) && params.Groups.length > 0 && Array.isArray(params.Groups[0].FilterParams)) {
    return params.Groups[0].FilterParams.map((filter, index) => ({
      id: `filter-${index}`,
      code: filter.param,
      defaultValue: filter.value || '',
      operation: filter.operation || '=',
    })).filter((row) => row.code);
  }
  return [];
}

function nextFilterRow() {
  const index = document.querySelectorAll('[data-fsg-filter-row]').length;
  const code = configuredFilterCodes()[0] || allAttributeCodes()[0] || '';
  return defaultFilterRowForCode(code, index);
}

function bindFilterRow(row) {
  const select = row.querySelector('[data-fsg-filter-param]');
  select?.addEventListener('change', () => refreshFilterValueOptions(row));
}

function addFilterRow() {
  const grid = field('fsgFilterGrid');
  if (!grid) return;
  grid.insertAdjacentHTML('beforeend', renderFilterRow(nextFilterRow(), parseQueryParams(selectedSavedQuery())));
  const rows = grid.querySelectorAll('[data-fsg-filter-row]');
  const row = rows[rows.length - 1];
  bindFilterRow(row);
  refreshFilterValueOptions(row);
}

function removeFilterRow(button) {
  const row = button.closest('[data-fsg-filter-row]');
  row?.remove();
}

function addFsgColumn(code) {
  if (!code || selectedColumnCodesFromDOM().includes(code)) return;
  field('fsgSelectedColumns')?.insertAdjacentHTML('beforeend', renderColumnChip(code));
  document.querySelector(`[data-add-fsg-param="${CSS.escape(code)}"]`)?.remove();
  rememberSelectedColumnOrder();
}

function removeFsgColumn(code) {
  document.querySelector(`[data-selected-fsg-param="${CSS.escape(code)}"]`)?.remove();
  if (document.querySelector(`[data-add-fsg-param="${CSS.escape(code)}"]`)) return;
  const targetID = isIndicatorColumn(code) ? 'fsgAvailableIndicators' : 'fsgAvailableAttributes';
  const target = field(targetID);
  target?.querySelector('.fsg-empty')?.remove();
  target?.insertAdjacentHTML('beforeend', renderAvailableColumnRow(code));
  rememberSelectedColumnOrder();
}

function moveColumnChip(draggedCode, targetCode) {
  if (!draggedCode || !targetCode || draggedCode === targetCode) return;
  const dragged = document.querySelector(`[data-selected-fsg-param="${CSS.escape(draggedCode)}"]`);
  const target = document.querySelector(`[data-selected-fsg-param="${CSS.escape(targetCode)}"]`);
  if (!dragged || !target) return;

  const chips = Array.from(document.querySelectorAll('[data-selected-fsg-param]'));
  const draggedIndex = chips.indexOf(dragged);
  const targetIndex = chips.indexOf(target);
  if (draggedIndex < targetIndex) {
    target.after(dragged);
  } else {
    target.before(dragged);
  }
  rememberSelectedColumnOrder();
}

async function ensureDictionaryValues(attrCode) {
  if (!attrCode || dictionaryValuesForAttr(attrCode).length > 0) {
    return;
  }
  const items = await request(`/dictionaries?business=FB&analytical_attribute_code=${encodeURIComponent(attrCode)}`);
  const knownKeys = new Set(state.dictionaryItems.map((item) => `${item.business}/${item.dictionary_code}/${item.code}`));
  items.forEach((item) => {
    const key = `${item.business}/${item.dictionary_code}/${item.code}`;
    if (!knownKeys.has(key)) state.dictionaryItems.push(item);
  });
}

async function refreshFilterValueOptions(row) {
  const paramSelect = row.querySelector('[data-fsg-filter-param]');
  const valueSelect = row.querySelector('[data-fsg-filter-value]');
  if (!paramSelect || !valueSelect) return;
  const attrCode = paramSelect.value;
  const previousValue = valueSelect.value || valueSelect.dataset.selectedValue || '';
  await ensureDictionaryValues(attrCode);
  valueSelect.innerHTML = dictionaryValueOptions(attrCode, previousValue);
}

function formatDateTime(value) {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ru-RU');
}

function executionStatusLabel(status) {
  const labels = {
    queued: 'В очереди',
    running: 'Выполняется',
    succeeded: 'Успешно',
    failed: 'Ошибка',
  };
  return labels[status] || status || '';
}

function selectedQueryExecutions() {
  const query = selectedSavedQuery();
  if (!query) return [];
  return state.queryExecutions.filter((execution) => execution.query_id === query.id);
}

function resultHistoryRows() {
  const resultsByID = new Map(state.selectedQueryResults.map((result) => [result.id, result]));
  const rows = selectedQueryExecutions().map((execution) => ({
    key: execution.id,
    execution,
    result: execution.result_id ? resultsByID.get(execution.result_id) : null,
  }));
  const executionResultIDs = new Set(rows.map((row) => row.execution.result_id).filter(Boolean));
  state.selectedQueryResults.forEach((result) => {
    if (!executionResultIDs.has(result.id)) {
      rows.push({ key: result.id, execution: null, result });
    }
  });
  return rows;
}

function resultIDForHistoryRow(execution, result) {
  return result?.id || execution?.result_id || '';
}

function canOpenHistoryResult(execution, result) {
  const status = execution?.status || (result ? 'succeeded' : '');
  return status === 'succeeded' && Boolean(resultIDForHistoryRow(execution, result));
}

async function loadResultRowsForHistory(resultID) {
  state.selectedHistoryResultId = resultID;
  state.selectedResultRows = [];
  state.selectedResultRowsError = '';
  state.selectedResultRowsLoading = true;
  render();
  try {
    const rows = await request(`/results/${encodeURIComponent(resultID)}/data?offset=0&limit=100`);
    state.selectedResultRows = Array.isArray(rows) ? rows : [];
  } catch (error) {
    state.selectedResultRowsError = error.message;
  } finally {
    state.selectedResultRowsLoading = false;
    render();
  }
}

function renderSelectedResultRowsTable(preferredColumnCodes = []) {
  if (!state.selectedHistoryResultId) {
    return `
      <section class="fsg-result-data">
        <div class="fsg-block-title">Строки результата</div>
        <div class="empty-cell">Выберите успешный запуск в таблице выше</div>
      </section>
    `;
  }
  if (state.selectedResultRowsLoading) {
    return `
      <section class="fsg-result-data">
        <div class="fsg-block-title">Строки результата</div>
        <div class="empty-cell">Загружаю строки результата ${escapeHtml(state.selectedHistoryResultId)}...</div>
      </section>
    `;
  }
  if (state.selectedResultRowsError) {
    return `
      <section class="fsg-result-data">
        <div class="fsg-block-title">Строки результата</div>
        <div class="status-line error">${escapeHtml(state.selectedResultRowsError)}</div>
      </section>
    `;
  }
  if (!state.selectedResultRows.length) {
    return `
      <section class="fsg-result-data">
        <div class="fsg-block-title">Строки результата</div>
        <div class="empty-cell">Для результата ${escapeHtml(state.selectedHistoryResultId)} нет строк</div>
      </section>
    `;
  }

  const selectedColumns = (preferredColumnCodes.length ? preferredColumnCodes : selectedColumnCodesFromDOM()).map((code) =>
    isIndicatorColumn(code) ? indicatorCodeFromColumn(code) : code,
  );
  const availableColumns = [...new Set(state.selectedResultRows.flatMap((row) => Object.keys(row)))];
  const columns = [
    ...selectedColumns.filter((column) => availableColumns.includes(column)),
    ...availableColumns.filter((column) => !selectedColumns.includes(column)),
  ];
  return `
    <section class="fsg-result-data">
      <div class="fsg-block-title">Строки результата <span class="mono-cell">${escapeHtml(state.selectedHistoryResultId)}</span></div>
      <div class="fsg-table-wrap">
        <table class="fsg-table fsg-data-table">
          <thead>
            <tr>${columns.map((column) => `<th>${escapeHtml(column)}</th>`).join('')}</tr>
          </thead>
          <tbody>
            ${state.selectedResultRows
              .map(
                (row) => `
                  <tr>
                    ${columns.map((column) => `<td>${escapeHtml(row[column] ?? '')}</td>`).join('')}
                  </tr>
                `,
              )
              .join('')}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function queryBodyFromForm() {
  const body = parseJson(value('queryPayload'), {});
  if (typeof body.params !== 'string') {
    body.params = JSON.stringify(body.params ?? {});
  }
  return body;
}

function executeBodyFromForm() {
  const body = {
    query_id: value('executeQueryId'),
    async: value('executeMode') !== 'sync',
    offset: Number(value('executeOffset') || 0),
    limit: Number(value('executeLimit') || 0),
    start_rep_date: value('executeStartDate') || undefined,
    end_rep_date: value('executeEndDate') || undefined,
    name: value('executeName') || undefined,
    description: value('executeDescription') || undefined,
    visibility: value('executeVisibility') || undefined,
    roles: value('executeRoles')
      ? value('executeRoles')
          .split(',')
          .map((role) => role.trim())
          .filter(Boolean)
      : undefined,
    org_code: value('executeOrgCode') || undefined,
  };
  Object.keys(body).forEach((key) => body[key] === undefined && delete body[key]);
  return body;
}

function selectedFsgVisualParams() {
  return selectedColumnCodesFromDOM().filter((code) => !isIndicatorColumn(code));
}

function selectedIndicatorColumns() {
  return selectedColumnCodesFromDOM().filter(isIndicatorColumn);
}

function selectedIndicatorValues(reportType = defaultReportType()) {
  const selected = selectedIndicatorColumns().map(indicatorCodeFromColumn);
  const forReportType = selected
    .filter((code) => code.startsWith(`${reportType}:`))
    .map((code) => code.split(':').slice(1).join(':'));
  if (forReportType.length) return forReportType;
  return selected.map((code) => code.split(':').pop()).filter(Boolean);
}

function fsgFiltersFromForm() {
  return Array.from(document.querySelectorAll('[data-fsg-filter-row]'))
    .map((row) => ({
      param: row.querySelector('[data-fsg-filter-param]')?.value || '',
      operation: row.querySelector('[data-fsg-filter-operation]')?.value || '',
      value: row.querySelector('[data-fsg-filter-value]')?.value || '',
    }))
    .filter((filter) => filter.param && filter.operation)
}

function fsgParamsFromForm() {
  const queryType = activeQueryType();
  const reportType = value('fsgReportType') || defaultReportType();
  const filterParams = fsgFiltersFromForm();
  const params = {
    VisualParams: selectedFsgVisualParams(),
    FilterParams: filterParams,
    RecType: Number(value('fsgRecType') || 1),
    StartRepDate: value('executeStartDate') || '2026-03-01',
    EndRepDate: value('executeEndDate') || '2026-03-31',
    NullStr: checked('fsgShowEmpty') ? 1 : 0,
    ReturnIfEmpty: 1,
  };
  if (queryType === 'FSG') {
    params.DateMode = Number(value('fsgDateMode') || 0);
    params.BeginBalance = Number(value('fsgBeginBalance') || 1);
    params.Book = value('fsgBook') || 'FTOperFed';
    params.Mode = Number(value('fsgMode') || 0);
    params.ResultParams = selectedIndicatorColumns().map(indicatorCodeFromColumn);
  } else if (queryType === 'TURN') {
    params.Book = value('fsgBook') || 'FTOperFed';
    params.ResultParams = selectedIndicatorColumns().map(indicatorCodeFromColumn);
  } else if (queryType === 'PA') {
    params.ReportType = reportType;
    params.Indicators = selectedIndicatorValues(reportType).join(',');
    params.Documents = 0;
    params.DateMode = Number(value('fsgDateMode') || 0);
    params.CurrencyCodeShow = 1;
  } else if (queryType === 'CONS') {
    params.ReportType = reportType;
    params.Groups = [{ indicators: selectedIndicatorValues(reportType).join(','), FilterParams: filterParams }];
    params.Documents = 0;
    params.DateMode = Number(value('fsgDateMode') || 0);
    params.CurrencyCodeShow = 1;
  }
  if (queryType === 'COR') {
    params.RptSetCode = Number(value('corRptSetCode') || 10);
    params.GroupByDocs = Number(value('corGroupByDocs') || 0);
    params.Book = value('fsgBook') || 'FTOperFed';
    params.ResultParams = selectedIndicatorColumns().map(indicatorCodeFromColumn);
    if (params.GroupByDocs === 1) {
      params.GroupByCorrespontents = Number(value('corGroupByCorrespontents') || 1);
    }
    const typeDoc = value('corTypeDoc');
    if (typeDoc) {
      params.TypeDoc = typeDoc
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
    }
  }
  return params;
}

function fsgQueryBodyFromForm() {
  const queryType = activeQueryType();
  return {
    name: value('executeName') || `${queryType} запрос`,
    description: value('executeDescription') || '',
    visibility: value('executeVisibility') || 'private',
    query_type: queryType,
    params: JSON.stringify(fsgParamsFromForm()),
  };
}

async function loadExecuteWorkspaceData(force = false) {
  if (state.executeDataLoading || (state.executeDataLoaded && !force)) return;
  state.executeDataLoading = true;
  setStatus('executeStatus', 'Загружаю сохраненные запросы...');
  try {
    const [queries, attrs, dictionaries, executions] = await Promise.all([
      request('/queries'),
      request('/analytical-attributes?business=FB'),
      request('/dictionaries?business=FB'),
      request('/query/executions'),
    ]);
    state.savedQueries = Array.isArray(queries) ? queries : [];
    state.analyticalAttributes = Array.isArray(attrs) ? attrs : [];
    state.dictionaryItems = Array.isArray(dictionaries) ? dictionaries : [];
    state.queryExecutions = Array.isArray(executions) ? executions : [];

    const queriesForType = activeSavedQueries();
    if (!queriesForType.some((query) => query.id === selectedQueryId())) {
      setSelectedQueryId(queriesForType[0]?.id || '');
    }

    const results = selectedQueryId() ? await request(`/queries/${selectedQueryId()}/results`) : [];
    state.selectedQueryResults = Array.isArray(results) ? results : [];

    state.executeDataLoaded = true;
    state.executeDataLoading = false;
    render();
  } catch (error) {
    state.executeDataLoading = false;
    setResult('executeResult', error.message);
    setStatus('executeStatus', 'Не удалось загрузить данные рабочего места', 'error');
  }
}

async function createFsgQueryFromWorkspace() {
  const saved = await request('/queries', { method: 'POST', body: JSON.stringify(fsgQueryBodyFromForm()) });
  setSelectedQueryId(saved.id);
  state.executeDataLoaded = false;
  await loadExecuteWorkspaceData(true);
  return saved;
}

function dictionaryBodyFromForm() {
  return {
    business: value('dictBusiness'),
    dictionary_code: value('dictCode'),
    code: value('dictItemCode'),
    short_name: value('dictShortName'),
    full_name: value('dictFullName'),
    analytical_attribute_code: value('dictAttrCode') || undefined,
  };
}

function dictionaryQueryString() {
  const params = new URLSearchParams();
  const pairs = {
    business: value('dictFilterBusiness'),
    dictionary_code: value('dictFilterCode'),
    q: value('dictSearch'),
    analytical_attribute_code: value('dictFilterAttr'),
  };
  Object.entries(pairs).forEach(([key, val]) => {
    if (val) params.set(key, val);
  });
  return params.toString();
}

function dictionaryItemQueryString() {
  const params = new URLSearchParams({
    business: value('dictBusiness'),
    dictionary_code: value('dictCode'),
    item_code: value('dictItemCode'),
  });
  return params.toString();
}

function render() {
  const sections = getSections();
  document.getElementById('app').innerHTML = `
    <div class="app-shell">
      <aside class="sidebar">
        <div class="brand">
          <h1>SVAP Query Service</h1>
          <p>Панель вызова API</p>
        </div>
        <nav class="nav">
          ${sections
            .map((item) => `<button data-section="${item.id}" class="${item.id === state.section ? 'active' : ''}" title="${escapeHtml(item.hint)}">${item.title}</button>`)
            .join('')}
        </nav>
      </aside>
      <main class="main">
        ${renderTopbar(sections)}
        ${renderQueries()}
        ${isQueryWorkspaceSection() ? renderExecute() : ''}
        ${renderExecutions()}
        ${renderResults()}
        ${renderDictionaries()}
        ${renderConfig()}
        ${renderInfo()}
      </main>
    </div>
  `;
  bindEvents();
  if (isQueryWorkspaceSection()) {
    loadExecuteWorkspaceData();
  }
}

function renderTopbar(sections) {
  if (isQueryWorkspaceSection()) return '';
  return `
    <div class="topbar">
      <div>
        <h2 id="pageTitle">${sections.find((item) => item.id === state.section)?.title}</h2>
        <p id="pageHint">${sections.find((item) => item.id === state.section)?.hint}</p>
      </div>
    </div>
  `;
}

async function loadReferenceData() {
  const [attrs, dictionaries] = await Promise.all([request('/analytical-attributes?business=FB'), request('/dictionaries?business=FB')]);
  state.analyticalAttributes = Array.isArray(attrs) ? attrs : [];
  state.dictionaryItems = Array.isArray(dictionaries) ? dictionaries : [];

  const querySections = configuredQueryTypes().map((type) => type.toLowerCase());
  if (!querySections.includes(state.section)) {
    state.section = querySections[0] || staticSections.queries.id;
  }
}

async function initializeApp() {
  document.getElementById('app').innerHTML = `
    <div class="app-shell">
      <main class="main">
        <div class="panel">
          <h3>Загрузка справочников</h3>
          <div class="status-line">Получаю конфигурацию интерфейса из БД...</div>
        </div>
      </main>
    </div>
  `;
  try {
    await loadReferenceData();
    render();
  } catch (error) {
    document.getElementById('app').innerHTML = `
      <div class="app-shell">
        <main class="main">
          <div class="panel">
            <h3>Не удалось загрузить справочники</h3>
            <div class="status-line error">${escapeHtml(error.message)}</div>
          </div>
        </main>
      </div>
    `;
  }
}

function sectionClass(id) {
  return `section ${state.section === id ? 'active' : ''}`;
}

function renderQueries() {
  return `
    <section id="section-queries" class="${sectionClass('queries')}">
      <div class="grid">
        <div class="panel">
          <h3>Создать запрос</h3>
          <textarea id="queryPayload">${stringify(defaultQueryPayloadFromDictionaries())}</textarea>
          <div class="actions">
            <button id="createQuery">POST /queries</button>
            <button id="listQueries" class="secondary">GET /queries</button>
          </div>
          <div id="queriesStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Получить или удалить</h3>
          <label>query id
            <input id="queryId" placeholder="UUID сохраненного запроса" />
          </label>
          <div class="actions">
            <button id="getQuery">GET /queries/{id}</button>
            <button id="deleteQuery" class="danger">DELETE /queries/{id}</button>
            <button id="listQueryResults" class="secondary">GET /queries/{id}/results</button>
          </div>
        </div>
      </div>
      <pre id="queriesResult" class="result"></pre>
    </section>
  `;
}

function renderExecute() {
  const queryType = activeQueryType();
  const queriesForType = activeSavedQueries();
  const query = selectedSavedQuery();
  const params = parseQueryParams(query);
  const columnParams = currentColumnCodes(params);
  const title = query?.name || '';
  const description = query?.description || '';
  const startDate = params.StartRepDate || '2026-03-01';
  const endDate = params.EndRepDate || '2026-03-31';
  const historyRows = resultHistoryRows();
  const allowedAttributeCodes = configuredAttributeColumnCodes();
  const allowedIndicatorCodes = configuredIndicatorColumnCodes();
  const allowedColumnCodes = [...allowedAttributeCodes, ...allowedIndicatorCodes];
  const storedColumnOrder = state.selectedColumnOrders[columnOrderKey(query?.id)] || [];
  const selectedColumnCodes = (storedColumnOrder.length ? storedColumnOrder : columnParams).filter((code) => allowedColumnCodes.includes(code));
  const selectedColumnSet = new Set(selectedColumnCodes);
  const availableAttributeCodes = allowedAttributeCodes.filter((code) => !selectedColumnSet.has(code));
  const availableIndicatorCodes = allowedIndicatorCodes.filter((code) => !selectedColumnSet.has(code));
  const filterRows = filterRowsForParams(params);

  return `
    <section id="section-query-workspace" class="section active">
      <div class="fsg-shell">
        <aside class="fsg-menu">
          <div class="fsg-logo">ФК РФ</div>
          <div class="fsg-query-list">
            ${
              queriesForType.length
                ? queriesForType
                    .map(
                      (item) => `
                        <button class="${item.id === query?.id ? 'active' : ''}" type="button" data-saved-query-id="${item.id}">
                          ${item.name}
                        </button>
                      `,
                    )
                    .join('')
                : `<div class="fsg-empty">Сохраненных ${queryType} запросов пока нет</div>`
            }
          </div>
          <div class="fsg-menu-footer">
            <button id="addFsgQuery" class="fsg-add" type="button" title="Создать запрос из текущих условий и колонок">+</button>
          </div>
        </aside>

        <div class="fsg-board">
          <header class="fsg-header">
            <div class="fsg-title-fields">
              <label class="span-2">name<input id="executeName" value="${title}" placeholder="${queryType} запрос" /></label>
              <label>С<input id="executeStartDate" value="${startDate}" /></label>
              <label>ПО<input id="executeEndDate" value="${endDate}" /></label>
              <input type="hidden" id="executeDescription" value="${description}" />
            </div>
            <div class="fsg-user">Петров Иван Иванович<br />6000 - УФК Саратов</div>
          </header>

          <div class="fsg-layout">
            <main class="fsg-main">
              <section class="fsg-block">
                <div class="fsg-block-title">Колонки</div>
                <div class="fsg-columns" id="fsgSelectedColumns">
                  ${selectedColumnCodes.map(renderColumnChip).join('') || '<span class="fsg-empty">Колонки не выбраны</span>'}
                </div>
              </section>

              <section class="fsg-block">
                <div class="fsg-block-head">
                  <div class="fsg-block-title">Условия выборки</div>
                  <button class="fsg-row-button" type="button" id="addFilterRow" title="Добавить условие">+</button>
                </div>
                <div class="fsg-filter-grid" id="fsgFilterGrid">
                  <span>Параметр</span><span>Операция</span><span>Значение</span><span></span>
                  ${filterRows.map((row) => renderFilterRow(row, params)).join('')}
                </div>
              </section>

              <details class="fsg-block fsg-runbar">
                <summary>Параметры запуска</summary>
                <div class="form-grid">
                  <label>query id<input id="executeQueryId" value="${query?.id || ''}" placeholder="UUID сохраненного запроса" /></label>
                  <label>mode
                    <select id="executeMode">
                      <option value="sync" selected>async = false</option>
                      <option value="async">async = true</option>
                    </select>
                  </label>
                  <label>offset<input id="executeOffset" type="number" min="0" value="0" /></label>
                  <label>limit<input id="executeLimit" type="number" min="0" max="1000" value="100" /></label>
                  <label>visibility
                    <select id="executeVisibility">
                      <option value="">из запроса</option>
                      <option value="private" ${query?.visibility === 'private' || !query ? 'selected' : ''}>private</option>
                      <option value="organization" ${query?.visibility === 'organization' ? 'selected' : ''}>organization</option>
                      <option value="public" ${query?.visibility === 'public' ? 'selected' : ''}>public</option>
                    </select>
                  </label>
                  <label>roles<input id="executeRoles" placeholder="admin,reader" /></label>
                  <label>org_code<input id="executeOrgCode" value="6000" /></label>
                  ${
                    queryType === 'COR'
                      ? `<label>RptSetCode<input id="corRptSetCode" type="number" value="${params.RptSetCode ?? 10}" /></label>
                        <label>GroupByDocs
                          <select id="corGroupByDocs">
                            <option value="0" ${params.GroupByDocs !== 1 ? 'selected' : ''}>0 - только счета</option>
                            <option value="1" ${params.GroupByDocs === 1 ? 'selected' : ''}>1 - документы</option>
                          </select>
                        </label>
                        <label>GroupByCorrespontents
                          <select id="corGroupByCorrespontents">
                            <option value="1" ${params.GroupByCorrespontents !== 0 ? 'selected' : ''}>1 - группировать</option>
                            <option value="0" ${params.GroupByCorrespontents === 0 ? 'selected' : ''}>0 - не группировать</option>
                          </select>
                        </label>
                        <label>TypeDoc<input id="corTypeDoc" value="${Array.isArray(params.TypeDoc) ? params.TypeDoc.join(',') : params.TypeDoc || ''}" placeholder="тип документа" /></label>`
                      : ''
                  }
                  ${
                    ['PA', 'CONS'].includes(queryType)
                      ? `<label>ReportType
                          <select id="fsgReportType">${reportTypeOptions(params.ReportType || defaultReportType())}</select>
                        </label>`
                      : ''
                  }
                  <label>Book<input id="fsgBook" value="${params.Book || 'FTOperFed'}" /></label>
                  <label>RecType<input id="fsgRecType" type="number" value="${params.RecType ?? 1}" /></label>
                  ${
                    queryType === 'FSG'
                      ? `<label>BeginBalance<input id="fsgBeginBalance" type="number" value="${params.BeginBalance ?? 1}" /></label>`
                      : ''
                  }
                  <label>Mode<input id="fsgMode" type="number" value="${params.Mode ?? 0}" /></label>
                  <label>DateMode<input id="fsgDateMode" type="number" value="${params.DateMode ?? 0}" /></label>
                  <label class="fsg-check"><input id="fsgShowEmpty" type="checkbox" ${params.NullStr ? 'checked' : ''} /> Пустые строки</label>
                </div>
              </details>
              <section class="fsg-actions-bar">
                <div class="actions">
                  <button id="previewFsgPayload" class="secondary">Показать payload</button>
                  <button id="refreshExecuteWorkspace" class="secondary">Обновить</button>
                  <button id="executeQuery">Рассчитать</button>
                </div>
                <div id="executeStatus" class="status-line"></div>
              </section>

              <section class="fsg-table-wrap">
                <table class="fsg-table">
                  <thead>
                    <tr>
                      <th>Статус</th>
                      <th>Период С</th>
                      <th>Период ПО</th>
                      <th>Режим</th>
                      <th>offset</th>
                      <th>limit</th>
                      <th>Задача</th>
                      <th>Результат</th>
                      <th>Создано</th>
                      <th>Старт</th>
                      <th>Завершено</th>
                      <th>Ошибка</th>
                    </tr>
                  </thead>
                  <tbody>
                    ${
                      historyRows.length
                        ? historyRows
                            .map(({ execution, result }) => {
                              const status = execution?.status || 'succeeded';
                              const resultID = resultIDForHistoryRow(execution, result);
                              const canOpen = canOpenHistoryResult(execution, result);
                              return `
                                <tr class="${canOpen ? 'result-history-row' : ''} ${state.selectedHistoryResultId === resultID ? 'selected' : ''}" ${canOpen ? `data-result-id="${escapeHtml(resultID)}"` : ''}>
                                  <td><span class="status-pill ${status}">${executionStatusLabel(status)}</span></td>
                                  <td>${execution?.start_rep_date || result?.meta?.StartRepDate || ''}</td>
                                  <td>${execution?.end_rep_date || result?.meta?.EndRepDate || ''}</td>
                                  <td>${execution?.mode || ''}</td>
                                  <td>${execution?.offset ?? ''}</td>
                                  <td>${execution?.limit ?? ''}</td>
                                  <td class="mono-cell">${execution?.id || ''}</td>
                                  <td class="mono-cell">${resultID}</td>
                                  <td>${formatDateTime(execution?.created_at || result?.fetched_at)}</td>
                                  <td>${formatDateTime(execution?.started_at)}</td>
                                  <td>${formatDateTime(execution?.finished_at || result?.fetched_at)}</td>
                                  <td>${execution?.error_message || ''}</td>
                                </tr>
                              `;
                            })
                            .join('')
                        : `<tr><td colspan="12" class="empty-cell">По выбранному запросу пока нет запусков и результатов</td></tr>`
                    }
                  </tbody>
                </table>
              </section>
              ${renderSelectedResultRowsTable(selectedColumnCodes)}
            </main>

            <aside class="fsg-side">
              <div class="fsg-side-group">
                <div class="fsg-block-title">Добавить АП</div>
                <div class="fsg-detail-columns" id="fsgAvailableAttributes">
                  ${renderAvailableColumnList(availableAttributeCodes, 'Все АП добавлены')}
                </div>
              </div>
              <div class="fsg-side-group">
                <div class="fsg-block-title">Добавить индикатор</div>
                <div class="fsg-detail-columns" id="fsgAvailableIndicators">
                  ${renderAvailableColumnList(availableIndicatorCodes, 'Все индикаторы добавлены')}
                </div>
              </div>
              <button class="secondary" type="button" id="loadDictionaryHints">Справочники</button>
            </aside>
          </div>
        </div>
      </div>
      <pre id="executeResult" class="result"></pre>
    </section>
  `;
}

function renderExecutions() {
  return `
    <section id="section-executions" class="${sectionClass('executions')}">
      <div class="grid">
        <div class="panel">
          <h3>Все задачи пользователя</h3>
          <button id="listExecutions">GET /query/executions</button>
          <div id="executionsStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Одна задача</h3>
          <label>execution id<input id="executionId" /></label>
          <button id="getExecution">GET /query/executions/{id}</button>
        </div>
      </div>
      <pre id="executionsResult" class="result"></pre>
    </section>
  `;
}

function renderResults() {
  return `
    <section id="section-results" class="${sectionClass('results')}">
      <div class="grid">
        <div class="panel">
          <h3>Результаты пользователя</h3>
          <button id="listResults">GET /results</button>
          <div id="resultsStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Данные или удаление результата</h3>
          <label>result id<input id="resultId" /></label>
          <div class="form-grid">
            <label>offset<input id="resultOffset" type="number" min="0" value="0" /></label>
            <label>limit<input id="resultLimit" type="number" min="0" max="1000" value="0" /></label>
          </div>
          <div class="actions">
            <button id="getResultData">GET /results/{id}/data</button>
            <button id="deleteResult" class="danger">DELETE /results/{id}</button>
          </div>
        </div>
      </div>
      <pre id="resultsResult" class="result"></pre>
    </section>
  `;
}

function renderDictionaries() {
  return `
    <section id="section-dictionaries" class="${sectionClass('dictionaries')}">
      <div class="grid">
        <div class="panel">
          <h3>Поиск</h3>
          <div class="form-grid">
            <label>business<input id="dictFilterBusiness" placeholder="FB" /></label>
            <label>dictionary_code<input id="dictFilterCode" placeholder="BUDGETS" /></label>
            <label>q<input id="dictSearch" placeholder="поиск по short_name/full_name" /></label>
            <label>analytical_attribute_code<input id="dictFilterAttr" placeholder="budgetCode" /></label>
          </div>
          <button id="listDictionaries">GET /dictionaries</button>
          <div id="dictStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Элемент справочника</h3>
          <div class="form-grid">
            <label>business<input id="dictBusiness" value="FB" /></label>
            <label>dictionary_code<input id="dictCode" value="BUDGETS" /></label>
            <label>code<input id="dictItemCode" value="99010001" /></label>
            <label>analytical_attribute_code<input id="dictAttrCode" value="budgetCode" /></label>
            <label>short_name<input id="dictShortName" value="Federal budget" /></label>
            <label>full_name<input id="dictFullName" value="Federal budget for revenue accounting" /></label>
          </div>
          <div class="actions">
            <button id="saveDictionary">POST /dictionaries</button>
            <button id="getDictionary" class="secondary">GET /dictionaries/item</button>
            <button id="deleteDictionary" class="danger">DELETE /dictionaries/item</button>
          </div>
        </div>
      </div>
      <pre id="dictResult" class="result"></pre>
    </section>
  `;
}

function renderConfig() {
  const sampleConfig = {
    server: { port: '8080' },
    retention: { default_ttl: '24h', role_ttls: {}, org_ttls: {}, user_ttls: {} },
  };
  return `
    <section id="section-config" class="${sectionClass('config')}">
      <div class="grid">
        <div class="panel">
          <h3>Просмотр</h3>
          <label>group name<input id="configGroup" placeholder="server, svap, retention" /></label>
          <div class="actions">
            <button id="getConfig">GET /config</button>
            <button id="listConfigGroups" class="secondary">GET /config/groups</button>
            <button id="getConfigGroup" class="secondary">GET /config/groups/{name}</button>
          </div>
          <div id="configStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Обновление</h3>
          <textarea id="configPayload">${stringify(sampleConfig)}</textarea>
          <div class="actions">
            <button id="updateConfig">PUT /config</button>
            <button id="applyConfig" class="secondary">POST /config/apply</button>
          </div>
        </div>
      </div>
      <pre id="configResult" class="result"></pre>
    </section>
  `;
}

function renderInfo() {
  return `
    <section id="section-info" class="${sectionClass('info')}">
      <div class="grid">
        <div class="panel">
          <h3>Настройки вызовов</h3>
          <div class="form-grid">
            <label>API base
              <input id="apiBase" value="${state.apiBase}" />
            </label>
            <label>X-User-ID
              <input id="userId" value="${state.userId}" />
            </label>
          </div>
          <div class="actions">
            <button id="saveSettings">Сохранить</button>
          </div>
          <div id="infoSettingsStatus" class="status-line"></div>
        </div>
        <div class="panel">
          <h3>Служебные endpoint’ы</h3>
          <div class="actions">
            <button id="getInfo">GET /info</button>
            <button id="openSwaggerInfo" class="secondary">Открыть /swagger/</button>
          </div>
          <div id="infoStatus" class="status-line"></div>
        </div>
      </div>
      <pre id="infoResult" class="result"></pre>
    </section>
  `;
}

function bindEvents() {
  document.querySelectorAll('[data-section]').forEach((button) => {
    button.addEventListener('click', () => {
      const previousSection = state.section;
      state.section = button.dataset.section;
      if (isQueryWorkspaceSection(previousSection) || isQueryWorkspaceSection(state.section)) {
        state.executeDataLoaded = false;
      }
      render();
    });
  });
  document.querySelectorAll('[data-saved-query-id]').forEach((button) => {
    button.addEventListener('click', () => {
      setSelectedQueryId(button.dataset.savedQueryId);
      state.executeDataLoaded = false;
      render();
    });
  });
  field('section-query-workspace')?.addEventListener('click', (event) => {
    const addButton = event.target.closest('[data-add-fsg-param]');
    if (addButton) {
      addFsgColumn(addButton.dataset.addFsgParam);
      return;
    }
    const removeButton = event.target.closest('[data-remove-fsg-param]');
    if (removeButton) {
      removeFsgColumn(removeButton.dataset.removeFsgParam);
      return;
    }
    const removeFilterButton = event.target.closest('[data-remove-filter-row]');
    if (removeFilterButton) {
      removeFilterRow(removeFilterButton);
      return;
    }
    const historyRow = event.target.closest('[data-result-id]');
    if (historyRow) {
      loadResultRowsForHistory(historyRow.dataset.resultId);
    }
  });
  field('fsgSelectedColumns')?.addEventListener('dragstart', (event) => {
    const chip = event.target.closest('[data-selected-fsg-param]');
    if (!chip) return;
    chip.classList.add('dragging');
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', chip.dataset.selectedFsgParam);
  });
  field('fsgSelectedColumns')?.addEventListener('dragover', (event) => {
    if (!event.target.closest('[data-selected-fsg-param]')) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  });
  field('fsgSelectedColumns')?.addEventListener('drop', (event) => {
    const target = event.target.closest('[data-selected-fsg-param]');
    if (!target) return;
    event.preventDefault();
    moveColumnChip(event.dataTransfer.getData('text/plain'), target.dataset.selectedFsgParam);
  });
  field('fsgSelectedColumns')?.addEventListener('dragend', () => {
    document.querySelectorAll('.fsg-column-label.dragging').forEach((chip) => chip.classList.remove('dragging'));
  });
  document.querySelectorAll('[data-fsg-filter-param]').forEach((select) => {
    bindFilterRow(select.closest('[data-fsg-filter-row]'));
  });
  field('addFilterRow')?.addEventListener('click', addFilterRow);

  field('saveSettings')?.addEventListener('click', updateGlobalSettings);
  field('openSwaggerInfo')?.addEventListener('click', () => window.open('/swagger/', '_blank'));

  field('createQuery')?.addEventListener('click', () =>
    run(async () => {
      const saved = await request('/queries', { method: 'POST', body: JSON.stringify(queryBodyFromForm()) });
      state.executeDataLoaded = false;
      return saved;
    }, 'queriesResult', 'queriesStatus'),
  );
  field('listQueries')?.addEventListener('click', () => run(() => request('/queries'), 'queriesResult', 'queriesStatus'));
  field('getQuery')?.addEventListener('click', () => run(() => request(`/queries/${value('queryId')}`), 'queriesResult', 'queriesStatus'));
  field('deleteQuery')?.addEventListener('click', () =>
    run(() => request(`/queries/${value('queryId')}`, { method: 'DELETE' }), 'queriesResult', 'queriesStatus'),
  );
  field('listQueryResults')?.addEventListener('click', () =>
    run(() => request(`/queries/${value('queryId')}/results`), 'queriesResult', 'queriesStatus'),
  );

  field('executeQuery')?.addEventListener('click', () =>
    run(async () => {
      const response = await request('/query/execute', { method: 'POST', body: JSON.stringify(executeBodyFromForm()) });
      const resultID = response?.result?.id || response?.execution?.result_id || '';
      state.executeDataLoaded = false;
      state.selectedHistoryResultId = '';
      state.selectedResultRows = [];
      state.selectedResultRowsError = '';
      await loadExecuteWorkspaceData(true);
      if (resultID) {
        await loadResultRowsForHistory(resultID);
      }
      return response;
    }, 'executeResult', 'executeStatus'),
  );
  field('addFsgQuery')?.addEventListener('click', () =>
    run(() => createFsgQueryFromWorkspace(), 'executeResult', 'executeStatus'),
  );
  field('refreshExecuteWorkspace')?.addEventListener('click', () => {
    state.executeDataLoaded = false;
    loadExecuteWorkspaceData(true);
  });
  field('previewFsgPayload')?.addEventListener('click', () => {
    setResult('executeResult', {
      create_query: fsgQueryBodyFromForm(),
      execute: executeBodyFromForm(),
    });
    setStatus('executeStatus', 'Payload сформирован', 'ok');
  });
  field('loadDictionaryHints')?.addEventListener('click', () => {
    const params = new URLSearchParams({
      dictionary_code: 'BUDGETS',
      analytical_attribute_code: 'budgetCode',
      q: value('fsgBudget'),
    });
    run(() => request(`/dictionaries?${params}`), 'executeResult', 'executeStatus');
  });

  field('listExecutions')?.addEventListener('click', () =>
    run(() => request('/query/executions'), 'executionsResult', 'executionsStatus'),
  );
  field('getExecution')?.addEventListener('click', () =>
    run(() => request(`/query/executions/${value('executionId')}`), 'executionsResult', 'executionsStatus'),
  );

  field('listResults')?.addEventListener('click', () => run(() => request('/results'), 'resultsResult', 'resultsStatus'));
  field('getResultData')?.addEventListener('click', () => {
    const params = new URLSearchParams();
    if (value('resultOffset')) params.set('offset', value('resultOffset'));
    if (value('resultLimit')) params.set('limit', value('resultLimit'));
    const suffix = params.toString() ? `?${params}` : '';
    run(() => request(`/results/${value('resultId')}/data${suffix}`), 'resultsResult', 'resultsStatus');
  });
  field('deleteResult')?.addEventListener('click', () =>
    run(() => request(`/results/${value('resultId')}`, { method: 'DELETE' }), 'resultsResult', 'resultsStatus'),
  );

  field('listDictionaries')?.addEventListener('click', () => {
    const params = dictionaryQueryString();
    run(() => request(`/dictionaries${params ? `?${params}` : ''}`), 'dictResult', 'dictStatus');
  });
  field('saveDictionary')?.addEventListener('click', () =>
    run(() => request('/dictionaries', { method: 'POST', body: JSON.stringify(dictionaryBodyFromForm()) }), 'dictResult', 'dictStatus'),
  );
  field('getDictionary')?.addEventListener('click', () =>
    run(() => request(`/dictionaries/item?${dictionaryItemQueryString()}`), 'dictResult', 'dictStatus'),
  );
  field('deleteDictionary')?.addEventListener('click', () =>
    run(() => request(`/dictionaries/item?${dictionaryItemQueryString()}`, { method: 'DELETE' }), 'dictResult', 'dictStatus'),
  );

  field('getConfig')?.addEventListener('click', () => run(() => request('/config'), 'configResult', 'configStatus'));
  field('listConfigGroups')?.addEventListener('click', () => run(() => request('/config/groups'), 'configResult', 'configStatus'));
  field('getConfigGroup')?.addEventListener('click', () =>
    run(() => request(`/config/groups/${value('configGroup')}`), 'configResult', 'configStatus'),
  );
  field('updateConfig')?.addEventListener('click', () =>
    run(() => request('/config', { method: 'PUT', body: JSON.stringify(parseJson(value('configPayload'), {})) }), 'configResult', 'configStatus'),
  );
  field('applyConfig')?.addEventListener('click', () =>
    run(() => request('/config/apply', { method: 'POST' }), 'configResult', 'configStatus'),
  );

  field('getInfo')?.addEventListener('click', () => run(() => request('/info'), 'infoResult', 'infoStatus'));
}

initializeApp();
