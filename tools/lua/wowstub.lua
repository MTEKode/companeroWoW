-- wowstub.lua: stubs minimos de globals de WoW para cargar los modulos del addon en LuaJIT.
-- Uso: require("wowstub") desde la raiz del repo (package.path = "tools/lua/?.lua;" .. package.path).
-- No simula comportamiento: solo evita errores al cargar y al llamar a metodos de frames.

bit = require("bit")

-- Objeto generico: cualquier metodo existe y devuelve el propio objeto.
local function newObject()
    local obj = {}
    setmetatable(obj, {
        __index = function(t, _)
            return function(self, ...)
                return self
            end
        end,
    })
    return obj
end

if CreateFrame == nil then
    function CreateFrame(_, _, _)
        return newObject()
    end
end

if GetPhysicalScreenSize == nil then
    function GetPhysicalScreenSize()
        return 1920, 1080
    end
end

-- issecretvalue NO se define aqui: el test lo instala cuando lo necesita.

return {
    newObject = newObject,
}
